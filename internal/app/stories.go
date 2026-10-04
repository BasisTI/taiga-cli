package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

func positive(selector string) (int64, error) {
	n, err := strconv.ParseInt(selector, 10, 64)
	if err != nil || n <= 0 {
		return 0, Usage("reference must be a positive integer")
	}
	return n, nil
}

// Story reads a story of the selected project by ref (userstories/by_ref) or by id. Tags stay
// as Taiga sent them, because the object is the baseline of a versioned write.
func (s *Service) Story(ctx context.Context, ref string, id int64) (Object, error) {
	return s.byRef(ctx, "userstories", "story", ref, id)
}

// StoryView is the output form: tag names instead of pairs, plus the web URL.
func (s *Service) StoryView(o Object) (Object, error) { return s.view(storyKind, o) }

func (s *Service) view(k kind, o Object) (Object, error) {
	out := Object{}
	for k, v := range o {
		out[k] = v
	}
	names, err := Names(o["tags"])
	if err != nil {
		return nil, err
	}
	out["tags"] = names
	out["url"] = s.API.BaseURL() + "/project/" + url.PathEscape(fmt.Sprint(s.Project["slug"])) + "/" + k.web + "/" + fmt.Sprint(o["ref"])
	return out, nil
}

// User resolves a user by exact username, id or "me" in users?project=, which also lists
// non-members (probe). Use it only where membership does not matter, like removing an assignee.
func (s *Service) User(ctx context.Context, selector string) (Object, error) {
	if selector == "me" {
		me, err := Read(ctx, s.API, "users/me", nil)
		if err != nil {
			return nil, err
		}
		selector = fmt.Sprint(me["id"])
	}
	users, err := s.Catalog(ctx, "users")
	if err != nil {
		return nil, err
	}
	return Resolve(users, selector, "username")
}

// Member resolves a project member by exact username, id or "me": the id must appear in the
// project's memberships.
func (s *Service) Member(ctx context.Context, selector string) (Object, error) {
	user, err := s.User(ctx, selector)
	if err != nil {
		return nil, err
	}
	members, err := s.Catalog(ctx, "memberships")
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if ID(m["user"]) > 0 && ID(m["user"]) == ID(user["id"]) {
			return user, nil
		}
	}
	return nil, &output.Error{Code: "not_found", Cause: "user is not a member of the project: " + selector, Exit: output.ExitNotFound}
}

// Epic resolves an epic of the project by ref only: a ref is never an id.
func (s *Service) Epic(ctx context.Context, ref string) (Object, error) {
	n, err := positive(ref)
	if err != nil {
		return nil, Usage("epic reference must be a positive integer")
	}
	items, err := s.Catalog(ctx, "epics")
	if err != nil {
		return nil, err
	}
	return resolve(items, strconv.FormatInt(n, 10), "ref", false)
}

// remoteFilters maps the list filters to the query parameters the probe proved Taiga honours.
// Taiga 6.7 reads the swimlane filter from the misspelled "swimnlane" and ignores "swimlane"
// (docs/api-notes.md); the local check stays the rule if a later version renames it.
var remoteFilters = map[string]string{"status": "status", "assignee": "assigned_users", "epic": "epic", "closed": "status__is_closed", "swimlane": "swimnlane"}

// Stories lists every story of the project matching filters (keys: ref, status, closed,
// search, assignee, epic, swimlane, tag; ids already resolved, swimlane "null" for none). Proven filters go to Taiga to shorten
// the list; every filter is then checked locally, so an ignored parameter never widens it.
func (s *Service) Stories(ctx context.Context, filters url.Values) ([]Object, error) {
	return s.items(ctx, storyKind, remoteFilters, filters)
}

// items lists the stories or tasks of the project matching filters: the proven ones (remote)
// go to Taiga, and every one is checked locally.
func (s *Service) items(ctx context.Context, k kind, remote map[string]string, filters url.Values) ([]Object, error) {
	q := url.Values{"project": {s.projectID()}}
	for key, param := range remote {
		if v, ok := filters[key]; ok {
			q.Set(param, v[0])
		}
	}
	raws, err := s.API.GetAll(ctx, k.base, q)
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	out := []Object{}
	for _, raw := range raws {
		o, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if ID(o["project"]) != ID(s.Project["id"]) {
			return nil, fmt.Errorf("list returned another project")
		}
		if _, ok := o["swimlane"]; !ok && filters.Has("swimlane") {
			return nil, fmt.Errorf("list returned a story without the swimlane field")
		}
		view, err := s.view(k, o)
		if err != nil {
			return nil, err
		}
		if matches(view, filters) {
			out = append(out, view)
		}
	}
	return out, nil
}

func matches(o Object, filters url.Values) bool {
	if v := filters.Get("ref"); v != "" && ID(o["ref"]) != ID(v) {
		return false
	}
	if v := filters.Get("status"); v != "" && fmt.Sprint(o["status"]) != v {
		return false
	}
	if v, ok := filters["closed"]; ok && fmt.Sprint(o["is_closed"]) != v[0] {
		return false
	}
	if v := filters.Get("search"); v != "" && !strings.Contains(strings.ToLower(fmt.Sprint(o["subject"])), strings.ToLower(v)) {
		return false
	}
	// A task has only assigned_to; a story shows assigned_to among its assigned_users too.
	if v := filters.Get("assignee"); v != "" && fmt.Sprint(o["assigned_to"]) != v && !contains(o["assigned_users"], func(x any) bool { return fmt.Sprint(x) == v }) {
		return false
	}
	if v := filters.Get("story"); v != "" && fmt.Sprint(o["user_story"]) != v {
		return false
	}
	if v := filters.Get("epic"); v != "" && !contains(o["epics"], func(x any) bool {
		e, ok := x.(map[string]any)
		return ok && fmt.Sprint(e["id"]) == v
	}) {
		return false
	}
	if v := filters.Get("swimlane"); v != "" {
		lane := "null"
		if o["swimlane"] != nil {
			lane = fmt.Sprint(o["swimlane"])
		}
		if lane != v {
			return false
		}
	}
	names, _ := o["tags"].([]string)
	for _, wanted := range filters["tag"] {
		found := false
		for _, n := range names {
			found = found || n == wanted
		}
		if !found {
			return false
		}
	}
	return true
}

func contains(list any, match func(any) bool) bool {
	xs, _ := list.([]any)
	for _, x := range xs {
		if match(x) {
			return true
		}
	}
	return false
}

// CreateStory posts body to the project, once and without version. When the answer says nothing
// conclusive (network after the connection opened, 5xx, 3xx), the result is always
// story_create_unconfirmed, naming the candidates found in the project (unconfirmedCreate). A
// re-run is never offered: it would create a second story.
func (s *Service) CreateStory(ctx context.Context, body Object, dry bool) (any, error) {
	subject, _ := body["subject"].(string)
	if strings.TrimSpace(subject) == "" {
		return nil, Usage("--subject is required")
	}
	body["project"] = s.Project["id"]
	if dry {
		return WritePlan{true, "POST", "userstories", body}, nil
	}
	me, err := Read(ctx, s.API, "users/me", nil)
	if err != nil {
		return nil, err
	}
	known, err := s.ownStories(ctx, me)
	if err != nil {
		return nil, err
	}
	r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: "userstories", Body: body})
	var ue *taiga.UnreadableBodyError
	if err != nil && !errors.As(err, &ue) && uncertain(err) {
		check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
		defer cancel()
		items, lerr := s.list(check, "userstories", url.Values{"owner": {fmt.Sprint(me["id"])}})
		return nil, unconfirmedCreate(storyKind, "the project", "`taiga story list`", items, lerr, known, me, body, err)
	}
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	created, err := Decode(r.Body)
	if err == nil && ID(created["id"]) <= 0 {
		err = fmt.Errorf("the response has no story id")
	}
	if err != nil {
		return nil, WriteApplied("POST", "userstories", r.Status, taiga.ToOutput(fmt.Errorf("decode the created story: %w", err)))
	}
	raw, err := reread(ctx, s.API, "POST", "userstories", fmt.Sprintf("userstories/%d", ID(created["id"])), r)
	if err != nil {
		return nil, err
	}
	if ID(raw["project"]) != ID(s.Project["id"]) || ID(raw["id"]) != ID(created["id"]) {
		return nil, WriteApplied("POST", "userstories", r.Status, fmt.Errorf("the re-read returned another story than id %d", ID(created["id"])))
	}
	return s.StoryView(raw)
}

// ownStories returns the ids of the stories of the project that me created. The owner filter only
// shortens the list; the owner is checked here too.
func (s *Service) ownStories(ctx context.Context, me Object) (map[int64]bool, error) {
	items, err := s.list(ctx, "userstories", url.Values{"owner": {fmt.Sprint(me["id"])}})
	if err != nil {
		return nil, err
	}
	ids := map[int64]bool{}
	for _, o := range items {
		if ID(o["owner"]) == ID(me["id"]) {
			ids[ID(o["id"])] = true
		}
	}
	return ids, nil
}

func (s *Service) UpdateStory(ctx context.Context, ref string, p Patch, dry, force bool) (any, error) {
	before, err := s.Story(ctx, ref, 0)
	if err != nil {
		return nil, err
	}
	return s.updateFrom(ctx, storyKind, before, p, dry, force)
}

func (s *Service) updateFrom(ctx context.Context, k kind, before Object, p Patch, dry, force bool) (any, error) {
	patch, err := BuildPatch(before, p)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/%d", k.base, ID(before["id"]))
	if k.fenceAssignees && opaque(patch) && !dry {
		return s.writeAssignees(ctx, path, before, p, patch, force)
	}
	result, err := s.Write(ctx, k, path, before, patch, dry, force)
	if err != nil {
		return nil, err
	}
	if o, ok := result.(Object); ok {
		return s.view(k, o)
	}
	return result, nil
}

// CloseStory moves the story to a closed status: the given one, or the project's only closed
// status. A story already closed is left alone unless another closed status is named.
func (s *Service) CloseStory(ctx context.Context, ref, selector string, dry, force bool) (any, error) {
	return s.close(ctx, storyKind, ref, selector, dry, force)
}

// close only changes the status: it never tags, archives or deletes.
func (s *Service) close(ctx context.Context, k kind, ref, selector string, dry, force bool) (any, error) {
	statuses, err := s.Catalog(ctx, k.statuses)
	if err != nil {
		return nil, err
	}
	var chosen Object
	if selector != "" {
		chosen, err = Resolve(statuses, selector, "name")
		if err != nil {
			return nil, err
		}
		if chosen["is_closed"] != true {
			return nil, Usage("close requires a closed status: " + selector)
		}
	}
	before, err := s.byRef(ctx, k.base, k.name, ref, 0)
	if err != nil {
		return nil, err
	}
	if chosen == nil {
		if before["is_closed"] == true {
			return s.view(k, before)
		}
		closed := []Object{}
		for _, st := range statuses {
			if st["is_closed"] == true {
				closed = append(closed, st)
			}
		}
		if len(closed) == 0 {
			return nil, &output.Error{Code: "not_found", Cause: "the project has no closed status", Exit: output.ExitNotFound}
		}
		if len(closed) > 1 {
			return nil, &output.Error{Code: "ambiguous_name", Cause: "the project has more than one closed status", Recovery: "choose one with --status", Exit: output.ExitUsage}
		}
		chosen = closed[0]
	}
	return s.updateFrom(ctx, k, before, Patch{Set: Object{"status": chosen["id"]}}, dry, force)
}

// writeAssignees writes a patch that changes the assignees. Taiga's OCC never sees assigned_to
// (docs/api-notes.md), so the write is fenced on both sides: the assignees are re-read right
// before the PATCH, and any change since the first read is a conflict; after the PATCH, the
// story is checked against the request, and the PATCH answer must be the next version of that
// re-read. A mismatch means the write landed next to someone else's: it is reported, never retried.
// --force-version skips both checks.
func (s *Service) writeAssignees(ctx context.Context, path string, before Object, p Patch, patch Object, force bool) (any, error) {
	base := before
	if !force {
		current, err := Read(ctx, s.API, path, nil)
		if err != nil {
			return nil, err
		}
		if !sameAssignees(before, current) {
			return nil, taiga.ToOutput(&taiga.ConflictError{Method: "PATCH", Path: path, Fields: opaqueKeys})
		}
		base = current
	}
	resp, err := s.send(ctx, storyKind, path, before, patch, force)
	if err != nil && uncertain(err) {
		return s.confirmAssignees(ctx, path, base, p, patch, err, force)
	}
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	after, err := reread(ctx, s.API, "PATCH", path, path, resp)
	if err != nil {
		return nil, err
	}
	if !force {
		problems := assigneeProblems(base, after, p)
		// The PATCH answer shows the version our write produced. When it cannot be read, the
		// re-read after the write stands in: it can only be larger, so the check stays sound
		// (at worst a write that landed after ours is reported too). It is never skipped.
		version, source := after["version"], "the re-read after it has"
		if written, err := Decode(resp.Body); err == nil {
			version, source = written["version"], "it answered"
		}
		if ID(version) != ID(base["version"])+1 {
			problems = append(problems, fmt.Sprintf("another write landed next to this PATCH (the read before it had version %v, %s version %v)", base["version"], source, version))
		}
		if len(problems) > 0 {
			return nil, assigneesMismatch(path, resp.Status, after, problems)
		}
	}
	return s.StoryView(after)
}

// confirmAssignees decides an assignee PATCH whose outcome is unknown by re-reading the story,
// with the checks of the answered write: the other fields sent show their value, the assignees
// match the request and, unless force, the version is the next one of base. Anything else is
// story_update_unconfirmed, exit 1: never a repeatable exit 7.
func (s *Service) confirmAssignees(ctx context.Context, path string, base Object, p Patch, patch Object, sendErr error, force bool) (any, error) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	now, err := Read(rctx, s.API, path, nil)
	checked := ""
	if err != nil {
		checked = "the re-read to check failed: " + output.AsError(err).Error()
	} else {
		others := Object{}
		for k, v := range patch {
			if !slices.Contains(opaqueKeys, k) {
				others[k] = v
			}
		}
		problems := append(differences(now, others), assigneeProblems(base, now, p)...)
		if !force && ID(now["version"]) != ID(base["version"])+1 {
			problems = append(problems, fmt.Sprintf("the version is %v, not %d", now["version"], ID(base["version"])+1))
		}
		if len(problems) == 0 {
			return s.StoryView(now)
		}
		checked = "the re-read does not match the request (" + strings.Join(problems, "; ") + "), but the request may still be running on the server or another write landed next to it"
	}
	return nil, &output.Error{Code: "story_update_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "PATCH " + path,
		Cause:    fmt.Sprintf("the change may have been applied: PATCH %s failed (%v) and %s", path, sendErr, checked),
		Recovery: fmt.Sprintf("do not re-run the command blindly: wait, check with `taiga story get %v` and repeat only what is still missing", base["ref"]),
		Exit:     output.ExitUnexpected}
}

func assigneesMismatch(path string, status int, after Object, problems []string) error {
	users, _ := assignees(after["assigned_users"])
	cause := fmt.Sprintf("the change was applied (PATCH %s returned HTTP %d), but the story does not match the request: %s; found assigned_to=%s assigned_users=%v (version %v)",
		path, status, strings.Join(problems, "; "), ownerName(ID(after["assigned_to"])), users, after["version"])
	return &output.Error{Code: "assignees_postcondition_failed", Source: "api", Stage: "PATCH " + path, Cause: cause,
		Recovery: "do not re-run the command blindly: someone else changed the assignees at the same time, which Taiga cannot detect for assigned_to; check the story with `taiga story get` and fix what is needed",
		Exit:     output.ExitConflict}
}
