package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// Read commands of US #253 (docs/api-notes.md, "Fase 3 — catálogos e épicos"). Every object
// they print goes through scrub: project logos, user photos and attachment links are signed
// media URLs whose token opens the file without authentication.

// scrub returns a deep copy of v with the value of every token= parameter hidden.
func scrub(v any) any {
	switch x := v.(type) {
	case string:
		return taiga.RedactTokens(x)
	case Object:
		out := Object{}
		for k, e := range x {
			out[k] = scrub(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = scrub(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = scrub(e)
		}
		return out
	}
	return v
}

// text is v when it is a string, else "": a null name must not match the search "nil".
func text(v any) string {
	s, _ := v.(string)
	return s
}

// found reports whether search is in one of values, ignoring case but not accents: "agil"
// does not find "Ágil" (decided for #253: no promise the tests do not cover).
func found(search string, values ...any) bool {
	if search == "" {
		return true
	}
	search = strings.ToLower(search)
	for _, v := range values {
		if strings.Contains(strings.ToLower(text(v)), search) {
			return true
		}
	}
	return false
}

// Projects lists the projects the account is a member of. Without member=, Taiga lists every
// project the account can see: public ones too, and the whole server for a superuser. The
// server's q= is a full-text search, not a substring, so search is applied here only.
func Projects(ctx context.Context, api API, search string) ([]Object, error) {
	me, err := Read(ctx, api, "users/me", nil)
	if err != nil {
		return nil, err
	}
	if ID(me["id"]) <= 0 {
		return nil, fmt.Errorf("users/me has no id")
	}
	raws, err := api.GetAll(ctx, "projects", url.Values{"member": {fmt.Sprint(ID(me["id"]))}})
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	out := []Object{}
	for _, raw := range raws {
		p, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if found(search, p["name"], p["slug"]) {
			out = append(out, scrub(p).(Object))
		}
	}
	return out, nil
}

// ProjectView is the selected project as read, with signed URLs hidden.
func (s *Service) ProjectView() Object { return scrub(s.Project).(Object) }

// Users lists the members of the project: users?project= also lists non-members, and
// memberships has no username, so both are read. Pending invitations (no user) are left out.
// Each user carries is_admin, role and role_name from its membership.
func (s *Service) Users(ctx context.Context, search string) ([]Object, error) {
	users, err := s.Catalog(ctx, "users")
	if err != nil {
		return nil, err
	}
	members, err := s.Catalog(ctx, "memberships")
	if err != nil {
		return nil, err
	}
	byUser := map[int64]Object{}
	for _, m := range members {
		if ID(m["user"]) > 0 {
			byUser[ID(m["user"])] = m
		}
	}
	out := []Object{}
	for _, u := range users {
		m, ok := byUser[ID(u["id"])]
		if !ok || ID(u["id"]) <= 0 || !found(search, u["username"], u["full_name"]) {
			continue
		}
		view := scrub(u).(Object)
		for _, k := range []string{"is_admin", "role", "role_name"} {
			view[k] = scrub(m[k])
		}
		out = append(out, view)
	}
	return out, nil
}

func closedQuery(param string, closed *bool) url.Values {
	if closed == nil {
		return nil
	}
	return url.Values{param: {strconv.FormatBool(*closed)}}
}

// Milestones lists the sprints of the project. closed, when set, goes to Taiga (it honours it)
// and is checked again here, with search on the name.
func (s *Service) Milestones(ctx context.Context, search string, closed *bool) ([]Object, error) {
	items, err := s.list(ctx, "milestones", closedQuery("closed", closed))
	if err != nil {
		return nil, err
	}
	out := []Object{}
	for _, m := range items {
		if (closed == nil || m["closed"] == *closed) && found(search, m["name"]) {
			out = append(out, scrub(m).(Object))
		}
	}
	return out, nil
}

// epicsEnabled refuses epic commands in a project whose epics module is off. Taiga still
// serves the epics then (probe); the switch is the project's statement that it does not use them.
func (s *Service) epicsEnabled() error {
	if s.Project["is_epics_activated"] == false {
		return &output.Error{Code: "not_found", Cause: "the epics module is disabled in this project",
			Recovery: "enable Epics in the project settings (Admin → Modules), or read them with `taiga api`", Exit: output.ExitNotFound}
	}
	return nil
}

// epicView is the output form of an epic: tag names, the web URL, signed URLs hidden.
func (s *Service) epicView(o Object) (Object, error) {
	view := scrub(o).(Object)
	names, err := Names(o["tags"])
	if err != nil {
		return nil, err
	}
	view["tags"] = names
	view["url"] = s.API.BaseURL() + "/project/" + url.PathEscape(fmt.Sprint(s.Project["slug"])) + "/epic/" + fmt.Sprint(o["ref"])
	return view, nil
}

// Epics lists the epics of the project; closed goes to Taiga as status__is_closed and is
// checked again here, search matches the subject (Taiga's q= is a full-text search).
func (s *Service) Epics(ctx context.Context, search string, closed *bool) ([]Object, error) {
	if err := s.epicsEnabled(); err != nil {
		return nil, err
	}
	items, err := s.list(ctx, "epics", closedQuery("status__is_closed", closed))
	if err != nil {
		return nil, err
	}
	out := []Object{}
	for _, e := range items {
		if (closed != nil && e["is_closed"] != *closed) || !found(search, e["subject"]) {
			continue
		}
		view, err := s.epicView(e)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// EpicDetail reads an epic by ref and adds user_stories: its linked stories in link order, each
// with id, ref, subject and project (plus project_slug when the story is in another project,
// which Taiga allows). A linked story the account cannot read keeps only its id.
func (s *Service) EpicDetail(ctx context.Context, ref string) (Object, error) {
	if err := s.epicsEnabled(); err != nil {
		return nil, err
	}
	e, err := s.byRef(ctx, "epics", "epic", ref, 0)
	if err != nil {
		return nil, err
	}
	view, err := s.epicView(e)
	if err != nil {
		return nil, err
	}
	id := ID(e["id"])
	raws, err := s.API.GetAll(ctx, fmt.Sprintf("epics/%d/related_userstories", id), nil)
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	links := []Object{}
	for _, raw := range raws {
		l, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if ID(l["epic"]) == id && ID(l["user_story"]) > 0 {
			links = append(links, l)
		}
	}
	sort.SliceStable(links, func(i, j int) bool { return ID(links[i]["order"]) < ID(links[j]["order"]) })
	stories := map[int64]Object{}
	if len(links) > 0 {
		raws, err = s.API.GetAll(ctx, "userstories", url.Values{"epic": {fmt.Sprint(id)}})
		if err != nil {
			return nil, taiga.ToOutput(err)
		}
		for _, raw := range raws {
			st, err := Decode(raw)
			if err != nil {
				return nil, err
			}
			stories[ID(st["id"])] = st
		}
	}
	linked := []Object{}
	for _, l := range links {
		entry := Object{"id": l["user_story"]}
		if st, ok := stories[ID(l["user_story"])]; ok {
			entry["ref"], entry["subject"], entry["project"] = st["ref"], scrub(st["subject"]), st["project"]
			if ID(st["project"]) != ID(s.Project["id"]) {
				info, _ := st["project_extra_info"].(map[string]any)
				entry["project_slug"] = scrub(info["slug"])
			}
		}
		linked = append(linked, entry)
	}
	view["user_stories"] = linked
	return view, nil
}
