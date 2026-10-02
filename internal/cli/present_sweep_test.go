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
		for _, st := range f.statuses {
			st["color"] = sweepURL
		}
		f.swimlanes = []map[string]any{{"id": 21, "name": "lane " + sweepURL, "order": 1, "project": 37}}
		for _, e := range f.history[6808] {
			e["user"] = user()
			e["comment"] = "comment " + sweepURL
		}
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
	} {
		cases = append(cases, struct {
			env   func() map[string]string
			stdin string
			args  []string
		}{func() map[string]string { _, env := story(); return env }, "", args})
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
	for _, args := range [][]string{{"project", "get"}, {"story", "get", "246"}, {"auth", "status"}} {
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
