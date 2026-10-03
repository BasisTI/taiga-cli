package cli

import (
	"net/http"
	"strings"
	"testing"
)

// sweepURL is a signed media URL whose token value is the sentinel.
const sweepURL = "http://t/media/user/x/photo.png?token=SWEEPSECRET"

// TestEveryCuratedCommandHidesTokens runs the curated commands against fakes in which every
// string Taiga could echo carries the sentinel (names, descriptions, tags, photos, comments,
// field values, error bodies), in JSON and in text, and looks for it in stdout and stderr.
func TestEveryCuratedCommandHidesTokens(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/no-bus")
	user := func() map[string]any {
		return map[string]any{"id": 5, "username": "admin", "photo": sweepURL, "big_photo": sweepURL}
	}
	story := func() (*storyFake, map[string]string) {
		f, _ := fieldFake(t)
		g, _ := commentFake(t)
		f.history = g.history
		for _, s := range f.stories {
			s["description"] = "see " + sweepURL + "\nnext line"
			s["owner_extra_info"], s["assigned_to_extra_info"] = user(), user()
			s["assigned_users_extra_info"] = []any{user()}
			s["tags"] = []any{[]any{"token=SWEEPSECRET", nil}}
		}
		for _, tk := range f.tasks {
			tk["subject"] = "task " + sweepURL
			tk["description"] = "see " + sweepURL + "\nnext line"
			tk["owner_extra_info"], tk["assigned_to_extra_info"] = user(), user()
			tk["tags"] = []any{[]any{"token=SWEEPSECRET", nil}}
		}
		for _, st := range append(f.statuses, f.taskStatuses...) {
			st["color"] = sweepURL
		}
		f.swimlanes = []map[string]any{{"id": 21, "name": "lane " + sweepURL, "order": 1, "project": 37}}
		for _, e := range f.epics {
			e["subject"] = "epic " + sweepURL
		}
		f.stories[6808]["epics"] = []any{map[string]any{"id": 90, "ref": 9, "subject": "epic " + sweepURL, "project": map[string]any{"id": 37, "slug": "infra-2025"}}}
		for _, e := range f.history[6808] {
			e["user"] = user()
			e["comment"] = "comment " + sweepURL
		}
		f.history[9100] = []map[string]any{{"id": "t1", "type": 1, "created_at": "2026-10-03T12:00:00.000Z", "comment": "task comment " + sweepURL, "user": user(),
			"diff": map[string]any{}, "delete_comment_date": nil, "edit_comment_date": nil}}
		f.defs["task-custom-attributes"][0]["description"] = sweepURL
		f.values["tasks/custom-attributes-values/9100"]["attributes_values"] = map[string]any{"27": sweepURL}
		f.defs["userstory-custom-attributes"][2]["description"] = sweepURL
		f.values[values6808]["attributes_values"] = map[string]any{"29": sweepURL}
		return f, f.env()
	}
	catalog := func() map[string]string {
		f, url, _ := newCatalogFake(t)
		f.evil = " " + sweepURL
		f.project["description"] = sweepURL
		return catalogEnv(url)
	}
	errSrv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte("denied for " + sweepURL))
	})
	errEnv := map[string]string{"TAIGA_URL": errSrv.URL, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}

	cases := []struct {
		env   func() map[string]string
		stdin string
		args  []string
	}{}
	for _, args := range [][]string{
		{"story", "get", "246"}, {"story", "list"}, {"story", "comments", "246", "--include-system"},
		{"story", "field", "list", "246"}, {"story", "field", "set", "246", "Notas=x"}, {"story", "field", "set", "246", "Notas=x", "--dry-run"},
		{"story", "field", "set", "246", "Data de entrega=" + sweepURL, "--dry-run"},
		{"story", "update", "246", "--subject", "s"}, {"story", "update", "246", "--append-description", "x", "--dry-run"},
		{"story", "close", "248", "--status", "Done"}, {"story", "comment", "246", "--body", sweepURL, "--dry-run"},
		{"story", "comment", "246", "--body", sweepURL}, {"status", "list"}, {"field", "list", "--kind", "story"}, {"swimlane", "list"},
		{"epic", "link", "91", "246"}, {"epic", "link", "91", "246", "--replace", "--confirm-delete"},
		{"epic", "link", "91", "246", "--replace", "--confirm-delete", "--dry-run"}, {"epic", "link", "9", "246"},
		{"story", "update", "246", "--subject", "s", "--epic", "91"}, {"story", "update", "246", "--replace-epic", "91", "--confirm-delete"},
		{"story", "update", "246", "--subject", "s", "--replace-epic", "91", "--confirm-delete", "--dry-run"},
		{"story", "create", "--subject", "s", "--epic", "91"}, {"story", "create", "--subject", "s", "--epic", "91", "--dry-run"},
		{"task", "get", "250"}, {"task", "list"}, {"task", "list", "--story", "246"}, {"task", "close", "250"}, {"task", "close", "250", "--dry-run"},
		{"task", "create", "--story", "246", "--subject", "s " + sweepURL}, {"task", "create", "--story", "246", "--subject", "s " + sweepURL, "--dry-run"},
		{"task", "update", "250", "--subject", "s"}, {"task", "update", "250", "--append-description", sweepURL, "--dry-run"},
		{"task", "update", "250", "--block", sweepURL},
		{"task", "field", "list", "250"}, {"task", "field", "set", "250", "Horas=x"}, {"task", "field", "set", "250", "Horas=" + sweepURL, "--dry-run"},
		{"task", "comments", "250"}, {"task", "comment", "250", "--body", sweepURL}, {"task", "comment", "250", "--body", sweepURL, "--dry-run"},
	} {
		cases = append(cases, struct {
			env   func() map[string]string
			stdin string
			args  []string
		}{func() map[string]string { _, env := story(); return env }, "", args})
	}
	// Link failures echo Taiga's error body in the cause.
	failing := func(request string, status int) func() map[string]string {
		return func() map[string]string {
			f, env := story()
			f.fail[request], f.failBody = status, `{"_error_message": "denied `+sweepURL+`"}`
			return env
		}
	}
	for _, c := range []struct {
		env  func() map[string]string
		args []string
	}{
		{failing("POST epics/9/related_userstories", 403), []string{"story", "create", "--subject", "s", "--epic", "91"}},
		{failing("POST epics/9/related_userstories", 502), []string{"story", "update", "246", "--subject", "s", "--epic", "91"}},
		{failing("DELETE epics/90/related_userstories/6808", 502), []string{"epic", "link", "91", "246", "--replace", "--confirm-delete"}},
		{failing("POST tasks", 502), []string{"task", "create", "--story", "246", "--subject", "s"}},
		{failing("POST tasks", 403), []string{"task", "create", "--story", "246", "--subject", "s"}},
		{failing("PATCH tasks/9100", 502), []string{"task", "update", "250", "--append-description", "x"}},
		{failing("PATCH tasks/9100", 302), []string{"task", "close", "250"}},
		{failing("PATCH tasks/9100", 502), []string{"task", "comment", "250", "--body", "x"}},
		{failing("PATCH tasks/custom-attributes-values/9100", 502), []string{"task", "field", "set", "250", "Horas=y"}},
	} {
		cases = append(cases, struct {
			env   func() map[string]string
			stdin string
			args  []string
		}{c.env, "", c.args})
	}
	cases = append(cases, struct {
		env   func() map[string]string
		stdin string
		args  []string
	}{func() map[string]string { _, env := story(); return env }, sweepURL, []string{"story", "create", "--subject", "s", "--description-file", "-", "--dry-run"}})
	for _, args := range [][]string{
		{"project", "list"}, {"project", "get"}, {"user", "list"}, {"milestone", "list"}, {"epic", "list"}, {"epic", "get", "9"},
		{"auth", "status", "--diagnose"},
	} {
		cases = append(cases, struct {
			env   func() map[string]string
			stdin string
			args  []string
		}{catalog, "", args})
	}
	for _, args := range [][]string{{"project", "get"}, {"story", "get", "246"}, {"auth", "status"}, {"epic", "link", "9", "246"},
		{"story", "update", "246", "--epic", "9"}, {"story", "create", "--subject", "s", "--epic", "9"},
		{"task", "get", "250"}, {"task", "list"}, {"task", "create", "--story", "246", "--subject", "s"}, {"task", "update", "250", "--subject", "s"},
		{"task", "field", "list", "250"}, {"task", "comments", "250"}, {"task", "comment", "250", "--body", "x"}} {
		cases = append(cases, struct {
			env   func() map[string]string
			stdin string
			args  []string
		}{func() map[string]string { return errEnv }, "", args})
	}
	for _, c := range cases {
		for _, mode := range []string{"json", "text"} {
			out, stderr, _ := runIn(t, c.env(), c.stdin, append(c.args, "--output", mode)...)
			if strings.Contains(out+stderr, "SWEEPSECRET") {
				t.Errorf("%v %s leaked:\nstdout: %s\nstderr: %s", c.args, mode, out, stderr)
			}
			if out+stderr == "" {
				t.Errorf("%v %s printed nothing", c.args, mode)
			}
		}
	}
}
