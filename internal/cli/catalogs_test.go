package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// catalogFake serves the read endpoints of US #253 for project 37 (slug infra-2025).
type catalogFake struct {
	project map[string]any
	evil    string
}

func newCatalogFake(t *testing.T) (*catalogFake, string, *[]recorded) {
	f := &catalogFake{project: map[string]any{"id": 37, "slug": "infra-2025", "name": "Infra 2025", "is_private": true,
		"i_am_member": true, "i_am_admin": false, "is_epics_activated": true, "is_kanban_activated": true, "is_backlog_activated": false,
		"logo_small_url": "http://t/media/logo.png?token=SECRET", "my_permissions": []any{}}}
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		path, q := strings.TrimPrefix(r.URL.Path, "/api/v1/"), r.URL.Query()
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case path == "projects/by_slug" && q.Get("slug") == "infra-2025", path == "projects/37":
			write(f.project)
		case path == "projects/by_slug", path == "projects/99":
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, `{"_error_message":"No Project matches the given query."}`)
		case path == "users/me":
			write(map[string]any{"id": 5, "username": "admin"})
		case path == "projects" && q.Get("member") == "5":
			write([]any{f.project, map[string]any{"id": 38, "slug": "outro", "name": "Outro" + f.evil, "i_am_member": true}})
		case path == "users" && q.Get("project") == "37":
			write([]any{map[string]any{"id": 5, "username": "admin", "full_name": "Administrador" + f.evil, "photo": "http://t/p.png?token=SECRET"},
				map[string]any{"id": 9, "username": "outsider", "full_name": "Fora"}})
		case path == "memberships" && q.Get("project") == "37":
			write([]any{map[string]any{"user": 5, "is_admin": true, "role_name": "Product Owner", "project": 37}})
		case path == "milestones" && q.Get("project") == "37":
			write([]any{map[string]any{"id": 1, "name": "Sprint 1" + f.evil, "slug": "sprint-1", "closed": true, "estimated_start": "2026-10-01", "estimated_finish": "2026-10-15", "project": 37, "user_stories": []any{}},
				map[string]any{"id": 2, "name": "Sprint 2", "slug": "sprint-2", "closed": false, "estimated_start": "2026-10-16", "estimated_finish": "2026-10-30", "project": 37}})
		case path == "epics" && q.Get("project") == "37":
			write([]any{map[string]any{"id": 90, "ref": 9, "project": 37, "subject": "Paridade" + f.evil, "status": 1, "status_extra_info": map[string]any{"name": "New"}, "is_closed": false, "color": "#abc", "tags": []any{}}})
		case path == "epics/by_ref" && q.Get("project") == "37" && q.Get("ref") == "9":
			write(map[string]any{"id": 90, "ref": 9, "project": 37, "version": 3, "subject": "Paridade", "description": "linha 1\nlinha 2" + f.evil, "status": 1, "is_closed": false, "color": "#abc", "tags": []any{[]any{"cli", nil}}})
		case path == "epics/by_ref":
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, `{"_error_message":"No Epic matches the given query."}`)
		case path == "epics/90/related_userstories":
			write([]any{map[string]any{"user_story": 6808, "epic": 90, "order": 1}, map[string]any{"user_story": 7001, "epic": 90, "order": 2}})
		case path == "userstories" && q.Get("epic") == "90":
			write([]any{map[string]any{"id": 6808, "ref": 246, "project": 37, "subject": "Stories"},
				map[string]any{"id": 7001, "ref": 12, "project": 38, "subject": "Outro", "project_extra_info": map[string]any{"slug": "outro"}}})
		default:
			t.Errorf("unexpected %s %s?%s", r.Method, path, r.URL.RawQuery)
			w.WriteHeader(404)
		}
	})
	return f, srv.URL, calls
}

func catalogEnv(url string) map[string]string {
	return map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
}

func TestProjectListNeedsNoProject(t *testing.T) {
	_, url, calls := newCatalogFake(t)
	out, stderr, code := runIn(t, map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": "tok"}, "", "project", "list")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 2 || strings.Contains(out, "SECRET") {
		t.Fatalf("%v %s", err, out)
	}
	text, _, code := runIn(t, map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": "tok"}, "", "project", "list", "--search", "OUT", "--output", "text")
	if code != 0 || strings.Join(strings.Fields(text), " ") != "id: 38 slug: outro name: Outro" {
		t.Fatalf("%d %q", code, text)
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("wrote: %+v", writes(calls))
	}
}

func TestProjectGetShowsSource(t *testing.T) {
	_, url, _ := newCatalogFake(t)
	for _, tc := range []struct {
		args   []string
		source string
	}{
		{[]string{"project", "get"}, "env:TAIGA_PROJECT"},
		{[]string{"project", "get", "--project", "37"}, "flag"},
		{[]string{"project", "get", "infra-2025"}, "argument"},
		{[]string{"project", "get", "37"}, "argument"},
	} {
		out, stderr, code := runIn(t, catalogEnv(url), "", tc.args...)
		if code != 0 {
			t.Fatalf("%v: %d %s", tc.args, code, stderr)
		}
		var p map[string]any
		if err := json.Unmarshal([]byte(out), &p); err != nil || p["slug"] != "infra-2025" || p["source"] != tc.source || strings.Contains(out, "SECRET") {
			t.Fatalf("%v: %v %s", tc.args, err, out)
		}
	}
	text, _, code := runIn(t, catalogEnv(url), "", "project", "get", "--output", "text")
	text = strings.Join(strings.Fields(text), " ")
	if code != 0 || !strings.Contains(text, "source: env:TAIGA_PROJECT") || !strings.Contains(text, "is_epics_activated: true") {
		t.Fatalf("%d %s", code, text)
	}
	_, stderr, code := runIn(t, catalogEnv(url), "", "project", "get", "99")
	if code != 5 || !strings.Contains(stderr, `"not_found"`) {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": "tok"}, "", "project", "get")
	if code != 2 || !strings.Contains(stderr, "no project selected") {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, catalogEnv(url), "", "project", "get", "a", "--project", "b")
	if code != 2 {
		t.Fatalf("argument and --project: %d %s", code, stderr)
	}
}

func TestUserListMembersOnly(t *testing.T) {
	_, url, _ := newCatalogFake(t)
	out, stderr, code := runIn(t, catalogEnv(url), "", "user", "list", "--output", "text")
	if code != 0 || strings.Join(strings.Fields(out), " ") != "username: admin full_name: Administrador id: 5 is_admin: true role_name: Product Owner" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	out, _, code = runIn(t, catalogEnv(url), "", "user", "list", "--search", "fora")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("non-member found: %d %s", code, out)
	}
	out, _, _ = runIn(t, catalogEnv(url), "", "user", "list")
	if strings.Contains(out, "SECRET") {
		t.Fatalf("%s", out)
	}
}

func TestMilestoneListClosedFilter(t *testing.T) {
	_, url, calls := newCatalogFake(t)
	out, stderr, code := runIn(t, catalogEnv(url), "", "milestone", "list", "--closed=false", "--output", "text")
	if code != 0 || strings.Join(strings.Fields(out), " ") != "id: 2 name: Sprint 2 slug: sprint-2 estimated_start: 2026-10-16 estimated_finish: 2026-10-30 closed: false" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	sent := false
	for _, c := range *calls {
		sent = sent || c.path == "/api/v1/milestones" && strings.Contains(c.query, "closed=false")
	}
	if !sent {
		t.Fatalf("closed not sent: %+v", *calls)
	}
	out, _, code = runIn(t, catalogEnv(url), "", "milestone", "list", "--search", "sprint 1")
	if code != 0 || !strings.Contains(out, `"user_stories"`) || strings.Count(out, `"slug"`) != 1 {
		t.Fatalf("%d %s", code, out)
	}
}

func TestEpicListAndGet(t *testing.T) {
	_, url, _ := newCatalogFake(t)
	out, stderr, code := runIn(t, catalogEnv(url), "", "epic", "list", "--output", "text")
	want := "ref: 9 subject: Paridade status: New (1) is_closed: false color: #abc url: " + url + "/project/infra-2025/epic/9"
	if code != 0 || strings.Join(strings.Fields(out), " ") != want {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	out, stderr, code = runIn(t, catalogEnv(url), "", "epic", "get", "9", "--output", "text")
	flat := strings.Join(strings.Fields(out), " ")
	if code != 0 || !strings.Contains(flat, "user_stories: 246, outro#12 url:") || !strings.Contains(flat, `description: "linha 1\nlinha 2"`) || !strings.Contains(flat, "tags: cli description:") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	out, _, code = runIn(t, catalogEnv(url), "", "epic", "get", "9")
	var e map[string]any
	if err := json.Unmarshal([]byte(out), &e); err != nil || code != 0 {
		t.Fatalf("%v %s", err, out)
	}
	if b, _ := json.Marshal(e["user_stories"]); string(b) != `[{"id":6808,"project":37,"ref":246,"subject":"Stories"},{"id":7001,"project":38,"project_slug":"outro","ref":12,"subject":"Outro"}]` {
		t.Fatalf("%s", b)
	}
	_, stderr, code = runIn(t, catalogEnv(url), "", "epic", "get", "246")
	if code != 5 || !strings.Contains(stderr, `"not_found"`) {
		t.Fatalf("story ref: %d %s", code, stderr)
	}
	for _, args := range [][]string{{"epic", "get"}, {"epic", "get", "x"}, {"epic", "get", "0"}, {"epic", "list", "--search", " "}, {"user", "list", "--search", ""}, {"milestone", "list", "a"}} {
		_, stderr, code := runIn(t, catalogEnv(url), "", args...)
		if code != 2 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestEpicCommandsWithModuleOff(t *testing.T) {
	f, url, calls := newCatalogFake(t)
	f.project["is_epics_activated"] = false
	for _, args := range [][]string{{"epic", "list"}, {"epic", "get", "9"}} {
		_, stderr, code := runIn(t, catalogEnv(url), "", args...)
		if code != 5 || !strings.Contains(stderr, "epics module is disabled") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	for _, c := range *calls {
		if strings.HasPrefix(c.path, "/api/v1/epics") {
			t.Fatalf("read epics with the module off: %+v", c)
		}
	}
}

func TestCatalogTextEscapesControls(t *testing.T) {
	f, url, _ := newCatalogFake(t)
	f.evil = "\x1b[31m\u202e\nid: 1"
	for _, args := range [][]string{
		{"project", "list"}, {"user", "list"}, {"milestone", "list"}, {"epic", "list"}, {"epic", "get", "9"},
	} {
		out, stderr, code := runIn(t, catalogEnv(url), "", append(args, "--output", "text")...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		if strings.ContainsAny(out, "\x1b\u202e") || strings.Contains(out, "\nid: 1") {
			t.Fatalf("%v: %q", args, out)
		}
	}
}

func TestProjectOutputHidesProjectCredentials(t *testing.T) {
	f, url, _ := newCatalogFake(t)
	for k, v := range map[string]any{"userstories_csv_uuid": "CSVSECRET-us", "tasks_csv_uuid": "CSVSECRET-t", "issues_csv_uuid": "CSVSECRET-i",
		"epics_csv_uuid": "CSVSECRET-e", "transfer_token": "5:TRANSFERSECRET"} {
		f.project[k] = v
	}
	for _, args := range [][]string{{"project", "get"}, {"project", "list"}, {"project", "get", "--output", "text"}, {"auth", "status", "--diagnose"}} {
		out, stderr, code := runIn(t, catalogEnv(url), "", args...)
		if code != 0 && args[0] != "auth" {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		if strings.Contains(out+stderr, "SECRET") {
			t.Fatalf("%v leaked a project credential: %s", args, out)
		}
	}
}

func TestProjectListKeepsOnlyMemberships(t *testing.T) {
	f, url, _ := newCatalogFake(t)
	f.project["i_am_member"] = false
	out, stderr, code := runIn(t, catalogEnv(url), "", "project", "list")
	if code != 0 || strings.Contains(out, "infra-2025") || !strings.Contains(out, `"outro"`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}

func TestEpicGetRejectsBadRefBeforeNetwork(t *testing.T) {
	_, url, calls := newCatalogFake(t)
	for _, ref := range []string{"abc", "0", "1.5", "-3"} {
		_, stderr, code := runIn(t, catalogEnv(url), "", "epic", "get", ref)
		if code != 2 || !strings.Contains(stderr, `"usage"`) {
			t.Fatalf("%s: %d %s", ref, code, stderr)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("bad ref reached the network: %+v", *calls)
	}
}

func TestProjectGetTextEscapesControls(t *testing.T) {
	f, url, _ := newCatalogFake(t)
	f.project["name"] = "Infra\x1b[31m\u202e\nsource: forged"
	out, stderr, code := runIn(t, catalogEnv(url), "", "project", "get", "--output", "text")
	if code != 0 || strings.ContainsAny(out, "\x1b\u202e") || strings.Contains(out, "\nsource: forged") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
}
