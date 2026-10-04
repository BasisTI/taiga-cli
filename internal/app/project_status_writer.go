package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// statusOrderCheckedWrite enables reordering WITHOUT optimistic concurrency, by human decision
// (US #249, 2026-10-01: "aceitar com conferência"). Taiga 6.7.3 has none for status order:
// statuses have no version, PATCH of order accepts any version and bulk_update_order has none
// (docs/api-notes.md, TestProbeStatusContract). Reorder re-reads the order right before its one
// bulk write and refuses if it moved since the plan, then re-reads after and reports a different
// order. A change landing between that read and the write is detected only when it leaves
// another order, and never prevented: it may be overwritten.
const statusOrderCheckedWrite = true

type statusHTTPWriter struct {
	service  *Service
	baseline map[string]Object
}

func (w *statusHTTPWriter) ProjectID() any { return w.service.Project["id"] }

// CheckPermission reads the project again and requires admin_project_values in my_permissions,
// which Taiga lists for project admins and superusers. A missing list is not a permission.
// It is a preflight only: the server's 403 still decides each write.
func (w *statusHTTPWriter) CheckPermission(ctx context.Context) error {
	project, err := Read(ctx, w.service.API, fmt.Sprintf("projects/%d", ID(w.service.Project["id"])), nil)
	if err != nil {
		return err
	}
	if permissions, ok := project["my_permissions"].([]any); ok {
		for _, p := range permissions {
			if p == "admin_project_values" {
				return nil
			}
		}
	}
	return &output.Error{Code: "forbidden", Source: "api", Cause: "admin_project_values permission is required",
		Recovery: "run with the account of a project admin", Exit: output.ExitForbidden}
}

func (w *statusHTTPWriter) loadStatuses(ctx context.Context) ([]Object, error) {
	delete(w.service.catalogs, "userstory-statuses")
	statuses, err := w.service.Catalog(ctx, "userstory-statuses")
	if err != nil {
		return nil, err
	}
	statuses = append(make([]Object, 0, len(statuses)), statuses...)
	SortStatuses(statuses)
	return statuses, nil
}

// Load reads statuses and story field definitions again, bypassing the run cache.
func (w *statusHTTPWriter) Load(ctx context.Context) ([]Object, []Object, error) {
	statuses, err := w.loadStatuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	delete(w.service.catalogs, "userstory-custom-attributes")
	fields, err := w.service.Fields(ctx, "story")
	if err != nil {
		return nil, nil, err
	}
	if w.baseline == nil {
		w.baseline = map[string]Object{}
		for _, st := range statuses {
			w.baseline[fmt.Sprint(st["name"])] = st
		}
	}
	return statuses, fields, nil
}

func (w *statusHTTPWriter) CheckReorderSupport(context.Context) error {
	if !statusOrderCheckedWrite {
		return Unsupported("reordering statuses is disabled: Taiga 6.7 has no optimistic concurrency for status order",
			"declare new statuses without `after`, or after the last status; reorder in the Taiga UI")
	}
	return nil
}

// changedStatus reports a status that appeared or changed between the plan and a write.
func changedStatus(name string) error { return changed("status", name) }

// changed reports a definition of kind that appeared or changed between the plan and a write.
func changed(kind, name string) error {
	return &output.Error{Code: "project_changed", Cause: fmt.Sprintf("%s %q changed in Taiga during apply", kind, name),
		Recovery: "run `taiga project plan` again and review it before applying", Exit: output.ExitConflict}
}

// existingStatus re-reads the catalog: nil when desired is still missing, nil error with the
// status when an equal one exists, and project_changed when a different one appeared.
func (w *statusHTTPWriter) existingStatus(ctx context.Context, desired Object) (Object, []Object, error) {
	statuses, err := w.loadStatuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	index, err := indexNames(statuses, "status")
	if err != nil {
		return nil, nil, err
	}
	name := fmt.Sprint(desired["name"])
	if caseVariant(statuses, name) != nil {
		return nil, nil, changedStatus(name)
	}
	st, found := index[name]
	if !found {
		return nil, statuses, nil
	}
	color, _ := st["color"].(string)
	if !strings.EqualFold(color, fmt.Sprint(desired["color"])) || st["is_closed"] != desired["is_closed"] {
		return nil, nil, changedStatus(name)
	}
	return st, statuses, nil
}

// CreateStatus re-reads the catalog, POSTs desired after the last status and re-reads the new
// status. An equal status already there is a no-op. The POST is never repeated: a lost answer
// is checked by reading the catalog, and stays write_applied if that does not settle it.
func (w *statusHTTPWriter) CreateStatus(ctx context.Context, desired Object) error {
	existing, statuses, err := w.existingStatus(ctx, desired)
	if existing != nil || err != nil {
		return err
	}
	body := Object{"project": w.service.Project["id"], "order": nextStatusOrder(statuses)}
	for k, v := range desired {
		body[k] = v
	}
	const path = "userstory-statuses"
	r, err := w.service.API.Do(ctx, taiga.Request{Method: "POST", Path: path, Body: body})
	if err != nil {
		// 400 on "name": refused, another run created it first (Taiga refuses the same name), so
		// a failed read is just that read's error. A 2xx with its body lost: the status is
		// saved, so a failed read must keep that (write_applied), never a repeatable error. Both
		// are settled by reading, never by a second POST.
		var ae *taiga.APIError
		var ue *taiga.UnreadableBodyError
		if errors.As(err, &ae) && ae.Status == 400 && hasKey(ae.Body, "name") {
			if existing, _, rerr := w.existingStatus(ctx, desired); existing != nil || rerr != nil {
				return rerr
			}
		}
		if errors.As(err, &ue) {
			existing, _, rerr := w.existingStatus(ctx, desired)
			var oe *output.Error
			switch {
			case existing != nil:
				return nil
			case errors.As(rerr, &oe) && oe.Code == "project_changed":
				return rerr // found, with other values: exit 4, never a repeatable one
			case rerr != nil:
				applied := *output.AsError(WriteApplied("POST", path, ue.Status, rerr))
				applied.Recovery = "do not assume it is missing: the status is saved; run `taiga project plan` to see what is left before applying again"
				return &applied
			}
		}
		if uncertain(err) {
			return w.statusUnconfirmed(ctx, fmt.Sprint(desired["name"]), path, err)
		}
		return taiga.ToOutput(err)
	}
	created, err := Decode(r.Body)
	if err == nil && ID(created["id"]) <= 0 {
		err = fmt.Errorf("the response has no status id")
	}
	if err != nil {
		return WriteApplied("POST", path, r.Status, taiga.ToOutput(fmt.Errorf("decode the created status: %w", err)))
	}
	confirmed, err := reread(ctx, w.service.API, "POST", path, fmt.Sprintf("%s/%d", path, ID(created["id"])), r)
	if err != nil {
		return err
	}
	if confirmed["name"] != desired["name"] {
		return changedStatus(fmt.Sprint(desired["name"]))
	}
	if w.baseline != nil {
		w.baseline[fmt.Sprint(confirmed["name"])] = confirmed
	}
	return nil
}

// statusUnconfirmed is status_create_unconfirmed, exit 1, for a POST whose outcome is unknown
// (network after the connection opened, 5xx, 3xx), like fieldUnconfirmed: a status with the name
// found afterwards is named, never adopted (option B). Taiga refuses a second status with the
// same name, so a later apply never duplicates it.
func (w *statusHTTPWriter) statusUnconfirmed(ctx context.Context, name, path string, sendErr error) error {
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	statuses, err := w.loadStatuses(check)
	checked := "the project has no status with this name yet, but the request may still be running on the server"
	if err != nil {
		checked = "the statuses could not be read to look for it: " + output.AsError(err).Error()
	} else {
		for _, st := range statuses {
			if st["name"] == name {
				checked = fmt.Sprintf("the project has a status with this name (id %d, color %v, closed %v), but nothing proves this command created it", ID(st["id"]), st["color"], st["is_closed"])
			}
		}
	}
	return &output.Error{Code: "status_create_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "POST " + path,
		Cause:    fmt.Sprintf("the status %q may have been created: POST %s failed (%v) and %s", name, path, sendErr, checked),
		Recovery: "do not assume it is missing: wait and run `taiga project plan` to see what is left; Taiga refuses a second status with the same name, so it is never created twice",
		Exit:     output.ExitUnexpected}
}

// CreateField re-reads the story fields, bypassing the run cache, and refuses a definition that
// appeared since the plan with another type or description, or with the name in another case.
// An equal one is a no-op; otherwise field create does the POST, never repeated.
func (w *statusHTTPWriter) CreateField(ctx context.Context, desired Object) error {
	name, description := fmt.Sprint(desired["name"]), fmt.Sprint(desired["description"])
	delete(w.service.catalogs, "userstory-custom-attributes")
	fields, err := w.service.Fields(ctx, "story")
	if err != nil {
		return err
	}
	index, err := indexNames(fields, "field")
	if err != nil {
		return err
	}
	if caseVariant(fields, name) != nil {
		return changed("field", name)
	}
	if f, found := index[name]; found {
		if f["type"] != desired["type"] || fmt.Sprint(f["description"]) != description {
			return changed("field", name)
		}
		return nil
	}
	_, err = w.service.CreateField(ctx, "story", name, fmt.Sprint(desired["type"]), &description, false)
	return err
}

// Reorder writes the whole order in one bulk_update_order request (one transaction on the
// server, so the board never shows half of it). It is not optimistic concurrency, see
// statusOrderCheckedWrite: right before the write it re-reads the catalog and refuses
// (project_changed, nothing sent) if any status appeared, vanished, was renamed or moved since
// the plan; right after, it re-reads and requires exactly names. The write is never repeated.
func (w *statusHTTPWriter) Reorder(ctx context.Context, names []string) error {
	if err := w.CheckReorderSupport(ctx); err != nil {
		return err
	}
	statuses, err := w.loadStatuses(ctx)
	if err != nil {
		return err
	}
	index, err := indexNames(statuses, "status")
	if err != nil {
		return err
	}
	if len(index) != len(w.baseline) || len(names) != len(index) {
		return changedStatus("catalog")
	}
	for name, old := range w.baseline {
		current, exists := index[name]
		if !exists || ID(current["id"]) != ID(old["id"]) || ID(current["order"]) != ID(old["order"]) {
			return changedStatus(name)
		}
	}
	pairs, missing := orderPairs(names, index)
	if missing {
		return changedStatus("catalog")
	}
	_, werr := w.service.API.Do(ctx, taiga.Request{Method: "POST", Path: statusOrderPath,
		Body: Object{"project": w.service.Project["id"], "bulk_userstory_statuses": pairs}})
	var ue *taiga.UnreadableBodyError
	lost := errors.As(werr, &ue) // applied, answer lost
	if werr != nil && !lost && !uncertain(werr) {
		return taiga.ToOutput(werr) // refused, or never sent: nothing was written
	}
	after, rerr := w.loadStatuses(ctx)
	if rerr != nil {
		if lost {
			return taiga.ToOutput(werr)
		}
		if werr != nil {
			return &output.Error{Code: "status_order_unconfirmed", Source: taiga.ToOutput(werr).Source, Stage: "POST " + statusOrderPath,
				Cause:    fmt.Sprintf("the order may have been applied: POST %s failed (%v) and the statuses could not be read to check: %s", statusOrderPath, werr, output.AsError(rerr).Error()),
				Recovery: "do not re-run blindly: wait, check `taiga status list`, then run `taiga project plan` and decide", Exit: output.ExitUnexpected}
		}
		return WriteApplied("POST", statusOrderPath, 204, rerr)
	}
	got := []string{}
	for _, st := range after {
		got = append(got, fmt.Sprint(st["name"]))
	}
	if reflect.DeepEqual(got, names) {
		return nil
	}
	outcome := "the order write was applied (HTTP 2xx)"
	if werr != nil {
		outcome = "the order write has an uncertain outcome (" + taiga.ToOutput(werr).Error() + ") and may have been applied"
	}
	return &output.Error{Code: "status_order_postcondition_failed", Source: "api", Stage: "POST " + statusOrderPath,
		Cause:    fmt.Sprintf("%s, but the statuses are now in another order: %v (wanted %v); another change landed next to it", outcome, got, names),
		Recovery: "do not re-run blindly: check `taiga status list`, then run `taiga project plan` and decide", Exit: output.ExitConflict}
}

// ProjectPlan reads the project's statuses and story fields and compares them with spec.
// It only reads: no permission is needed beyond seeing the project.
func (s *Service) ProjectPlan(ctx context.Context, spec ProjectSpec) (ProjectPlan, error) {
	statuses, fields, err := (&statusHTTPWriter{service: s}).Load(ctx)
	if err != nil {
		return ProjectPlan{}, err
	}
	return BuildProjectPlan(spec, statuses, fields)
}

// ApplyProject runs Apply against Taiga.
func (s *Service) ApplyProject(ctx context.Context, spec ProjectSpec, dry bool) (ApplyResult, error) {
	result, err := Apply(ctx, &statusHTTPWriter{service: s}, spec, dry)
	if err != nil {
		e := taiga.ToOutput(err)
		// Part of the plan is saved: a later failure (a read, mostly) must not look like a
		// repeatable network error.
		if len(result.Applied) > 0 && e.Exit == output.ExitNetwork {
			return result, &output.Error{Code: "project_apply_interrupted", Source: e.Source, Stage: e.Stage,
				Cause:    fmt.Sprintf("%d action(s) were applied (see applied), then [%s] %s", len(result.Applied), e.Code, e.Cause),
				Recovery: "do not assume nothing was saved: run `taiga project plan` to see what is left, then apply again (it re-plans and never creates a name twice)",
				Exit:     output.ExitUnexpected}
		}
		return result, e
	}
	return result, nil
}
