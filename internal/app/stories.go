package app

import (
	"context"
	"fmt"
	"net/url"
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
	if (ref == "") == (id == 0) {
		return nil, Usage("choose REF or --id")
	}
	path, q := "userstories/by_ref", url.Values{}
	var expected int64
	if ref != "" {
		var err error
		expected, err = positive(ref)
		if err != nil {
			return nil, err
		}
		q.Set("project", s.projectID())
		q.Set("ref", fmt.Sprint(expected))
	} else {
		if id <= 0 {
			return nil, Usage("id must be positive")
		}
		path, q = fmt.Sprintf("userstories/%d", id), nil
	}
	o, err := Read(ctx, s.API, path, q)
	if err != nil {
		return nil, err
	}
	if ID(o["project"]) != ID(s.Project["id"]) {
		return nil, Usage(fmt.Sprintf("story belongs to another project, not %v", s.Project["slug"]))
	}
	if ID(o["id"]) <= 0 || ID(o["ref"]) <= 0 {
		return nil, fmt.Errorf("invalid story identity")
	}
	if expected > 0 && ID(o["ref"]) != expected {
		return nil, fmt.Errorf("by_ref returned another reference")
	}
	if id > 0 && ID(o["id"]) != id {
		return nil, fmt.Errorf("GET returned another id")
	}
	return o, nil
}

// StoryView is the output form: tag names instead of pairs, plus the web URL.
func (s *Service) StoryView(o Object) (Object, error) {
	out := Object{}
	for k, v := range o {
		out[k] = v
	}
	names, err := Names(o["tags"])
	if err != nil {
		return nil, err
	}
	out["tags"] = names
	out["url"] = s.API.BaseURL() + "/project/" + url.PathEscape(fmt.Sprint(s.Project["slug"])) + "/us/" + fmt.Sprint(o["ref"])
	return out, nil
}

// Member resolves a project member by exact username, id or "me". users?project= also lists
// non-members (probe), so the id must appear in the project's memberships.
func (s *Service) Member(ctx context.Context, selector string) (Object, error) {
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
	user, err := Resolve(users, selector, "username")
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
	if _, err := positive(ref); err != nil {
		return nil, Usage("epic reference must be a positive integer")
	}
	items, err := s.Catalog(ctx, "epics")
	if err != nil {
		return nil, err
	}
	return resolve(items, ref, "ref", false)
}

// remoteFilters maps the list filters to the query parameters the probe proved Taiga honours.
var remoteFilters = map[string]string{"status": "status", "assignee": "assigned_users", "epic": "epic", "closed": "status__is_closed"}

// Stories lists every story of the project matching filters (keys: ref, status, closed,
// search, assignee, epic, tag; ids already resolved). Proven filters go to Taiga to shorten
// the list; every filter is then checked locally, so an ignored parameter never widens it.
func (s *Service) Stories(ctx context.Context, filters url.Values) ([]Object, error) {
	q := url.Values{"project": {s.projectID()}}
	for key, param := range remoteFilters {
		if v, ok := filters[key]; ok {
			q.Set(param, v[0])
		}
	}
	raws, err := s.API.GetAll(ctx, "userstories", q)
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
		view, err := s.StoryView(o)
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
	if v := filters.Get("ref"); v != "" && fmt.Sprint(o["ref"]) != v {
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
	if v := filters.Get("assignee"); v != "" && !contains(o["assigned_users"], func(x any) bool { return fmt.Sprint(x) == v }) {
		return false
	}
	if v := filters.Get("epic"); v != "" && !contains(o["epics"], func(x any) bool {
		e, ok := x.(map[string]any)
		return ok && fmt.Sprint(e["id"]) == v
	}) {
		return false
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

// CreateStory posts body to the project. There is no previous version: none is sent.
func (s *Service) CreateStory(ctx context.Context, body Object, dry bool) (any, error) {
	subject, _ := body["subject"].(string)
	if strings.TrimSpace(subject) == "" {
		return nil, Usage("--subject is required")
	}
	body["project"] = s.Project["id"]
	if dry {
		return WritePlan{true, "POST", "userstories", body}, nil
	}
	r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: "userstories", Body: body})
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	created, err := Decode(r.Body)
	if err != nil || ID(created["id"]) <= 0 {
		return nil, fmt.Errorf("POST userstories succeeded but returned no story id; do not re-run, look the story up with `taiga story list`")
	}
	raw, err := reread(ctx, s.API, fmt.Sprintf("userstories/%d", ID(created["id"])), r)
	if err != nil {
		return nil, err
	}
	if ID(raw["project"]) != ID(s.Project["id"]) || ID(raw["id"]) != ID(created["id"]) {
		return nil, fmt.Errorf("re-read of the created story returned another story")
	}
	return s.StoryView(raw)
}

func (s *Service) UpdateStory(ctx context.Context, ref string, p Patch, dry, force bool) (any, error) {
	before, err := s.Story(ctx, ref, 0)
	if err != nil {
		return nil, err
	}
	return s.updateFrom(ctx, before, p, dry, force)
}

func (s *Service) updateFrom(ctx context.Context, before Object, p Patch, dry, force bool) (any, error) {
	patch, err := BuildPatch(before, p)
	if err != nil {
		return nil, err
	}
	result, err := s.Write(ctx, fmt.Sprintf("userstories/%d", ID(before["id"])), before, patch, dry, force)
	if err != nil {
		return nil, err
	}
	if o, ok := result.(Object); ok {
		return s.StoryView(o)
	}
	return result, nil
}

// CloseStory moves the story to a closed status: the given one, or the project's only closed
// status. A story already closed is left alone unless another closed status is named.
func (s *Service) CloseStory(ctx context.Context, ref, selector string, dry, force bool) (any, error) {
	statuses, err := s.Catalog(ctx, "userstory-statuses")
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
	before, err := s.Story(ctx, ref, 0)
	if err != nil {
		return nil, err
	}
	if chosen == nil {
		if before["is_closed"] == true {
			return s.StoryView(before)
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
	return s.updateFrom(ctx, before, Patch{Set: Object{"status": chosen["id"]}}, dry, force)
}
