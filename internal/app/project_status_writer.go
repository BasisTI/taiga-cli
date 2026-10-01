package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// versionedStatusOrderValidated stays false: on Taiga 6.7.3 statuses have no version, PATCH of
// order accepts any version and bulk_update_order has none (docs/api-notes.md, TestProbeStatusContract).
// Turn it on only after a human decision and local evidence of a versioned order write.
const versionedStatusOrderValidated = false

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
	if !versionedStatusOrderValidated {
		return Unsupported("reordering statuses is not supported: Taiga 6.7 has no optimistic concurrency for status order (statuses have no version)",
			"declare new statuses without `after`, or after the last status, so they are created at the end; reorder in the Taiga UI")
	}
	for _, st := range w.baseline {
		if _, exists := st["version"]; !exists {
			return Unsupported("status resource has no version", "see docs/api-notes.md")
		}
	}
	return nil
}

// changedStatus reports a status that appeared or changed between the plan and a write.
func changedStatus(name string) error {
	return &output.Error{Code: "project_changed", Cause: fmt.Sprintf("status %q changed in Taiga during apply", name),
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
	st, found := index[name]
	if !found {
		if caseVariant(statuses, name) != nil {
			return nil, nil, changedStatus(name)
		}
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
		// 400 on "name": another run created it first (Taiga refuses the same name). A lost
		// answer: the status may exist. Both are settled by reading, never by a second POST.
		var ae *taiga.APIError
		var ue *taiga.UnreadableBodyError
		if (errors.As(err, &ae) && ae.Status == 400 && hasKey(ae.Body, "name")) || errors.As(err, &ue) {
			if existing, _, rerr := w.existingStatus(ctx, desired); existing != nil || rerr != nil {
				return rerr
			}
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

// CreateField reuses field create: an equal definition is a no-op, a different one an error.
func (w *statusHTTPWriter) CreateField(ctx context.Context, desired Object) error {
	description := fmt.Sprint(desired["description"])
	_, err := w.service.CreateField(ctx, "story", fmt.Sprint(desired["name"]), fmt.Sprint(desired["type"]), &description, false)
	return err
}

// Reorder writes only order, one status at a time, each with the version of the status read
// for the plan. It runs only when versionedStatusOrderValidated is set.
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
	if len(index) != len(names) {
		return changedStatus("catalog")
	}
	for _, name := range names {
		current, exists := index[name]
		old, known := w.baseline[name]
		if !exists || !known || ID(current["id"]) != ID(old["id"]) || !equal(old["order"], current["order"]) {
			return changedStatus(name)
		}
	}
	for i, name := range names {
		old := w.baseline[name]
		if ID(old["order"]) == int64(i) {
			continue
		}
		raw, err := Snapshot(old)
		if err != nil {
			return err
		}
		if _, err := w.service.API.WriteVersionedFrom(ctx, "PATCH", fmt.Sprintf("userstory-statuses/%d", ID(old["id"])),
			map[string]any{"order": i}, raw, false); err != nil {
			return taiga.ToOutput(err)
		}
	}
	return nil
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
		return result, taiga.ToOutput(err)
	}
	return result, nil
}
