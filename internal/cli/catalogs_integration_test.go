//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func catalogList(t *testing.T, env map[string]string, args ...string) []map[string]any {
	t.Helper()
	out, errOut, code := runIn(t, env, "", args...)
	if code != 0 {
		t.Fatalf("%v: %d %s", args, code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("%v: %v %s", args, err, out)
	}
	return items
}

func fieldOf(items []map[string]any, key string) string {
	out := []string{}
	for _, it := range items {
		out = append(out, fmt.Sprint(it[key]))
	}
	return strings.Join(out, ",")
}

// TestIntegrationCatalogCommands reads projects, members, milestones and epics as svc, a plain
// member, in a project of its own (admin prepares it through `taiga api`).
func TestIntegrationCatalogCommands(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, svc, pid := freshProject(t, "cli-test-catalogs-"+suffix)
	slug := env["TAIGA_PROJECT"]
	storyJSON(t, env, "", "api", "PATCH", "projects/"+pid, "-F", "is_epics_activated=true")

	// project list: only memberships; a project svc is not in stays out.
	other := storyJSON(t, env, "", "api", "POST", "projects", "-f", "name=cli-test-catalogs-other-"+suffix, "-f", "description=taiga-cli")
	projects := catalogList(t, svc, "project", "list", "--search", suffix)
	if fieldOf(projects, "slug") != slug {
		t.Fatalf("svc project list: %s", fieldOf(projects, "slug"))
	}
	if all := catalogList(t, env, "project", "list", "--search", "CLI-TEST-CATALOGS-OTHER-"+suffix); fieldOf(all, "id") != fmt.Sprint(other["id"]) {
		t.Fatalf("admin project list --search: %s", fieldOf(all, "id"))
	}

	// project get: selected project with its source, explicit argument, project svc cannot see.
	p := storyJSON(t, svc, "", "project", "get")
	if p["slug"] != slug || p["source"] != "env:TAIGA_PROJECT" || p["i_am_member"] != true || p["i_am_admin"] != false {
		t.Fatalf("project get: %v", p)
	}
	if p := storyJSON(t, svc, "", "project", "get", pid); p["slug"] != slug || p["source"] != "argument" {
		t.Fatalf("project get ID: %v", p)
	}
	for _, sel := range []string{fmt.Sprint(other["slug"]), fmt.Sprint(other["id"])} {
		_, errOut, code := runIn(t, svc, "", "project", "get", sel)
		if code != 5 && code != 6 {
			t.Fatalf("project get %s (private, not a member): %d %s", sel, code, errOut)
		}
	}

	// user list: members only, with the role.
	users := catalogList(t, svc, "user", "list")
	if fieldOf(users, "username") != "admin,svc" && fieldOf(users, "username") != "svc,admin" {
		t.Fatalf("user list: %v", users)
	}
	for _, u := range users {
		if u["role_name"] == nil || (u["username"] == testtaiga.ServiceUser) == (u["is_admin"] == true) {
			t.Fatalf("user list roles: %v", users)
		}
	}
	if found := catalogList(t, svc, "user", "list", "--search", "SV"); fieldOf(found, "username") != "svc" {
		t.Fatalf("user list --search: %v", found)
	}

	// milestone list: closed filter.
	for i, closed := range []bool{true, false} {
		m := storyJSON(t, env, "", "api", "POST", "milestones", "-F", "project="+pid, "-f", fmt.Sprintf("name=Sprint %d", i+1),
			"-f", "estimated_start=2026-10-01", "-f", "estimated_finish=2026-10-15")
		if closed {
			storyJSON(t, env, "", "api", "PATCH", fmt.Sprintf("milestones/%v", m["id"]), "-F", "closed=true")
		}
	}
	if ms := catalogList(t, svc, "milestone", "list"); fieldOf(ms, "name") != "Sprint 1,Sprint 2" && fieldOf(ms, "name") != "Sprint 2,Sprint 1" {
		t.Fatalf("milestone list: %s", fieldOf(ms, "name"))
	}
	if ms := catalogList(t, svc, "milestone", "list", "--closed"); fieldOf(ms, "name") != "Sprint 1" {
		t.Fatalf("milestone list --closed: %s", fieldOf(ms, "name"))
	}
	if ms := catalogList(t, svc, "milestone", "list", "--closed=false", "--search", "sprint"); fieldOf(ms, "name") != "Sprint 2" {
		t.Fatalf("milestone list --closed=false: %s", fieldOf(ms, "name"))
	}

	// epic list/get: linked stories of this project and of another one.
	e1 := storyJSON(t, env, "", "api", "POST", "epics", "-F", "project="+pid, "-f", "subject=Paridade "+suffix, "-F", `tags=["cli"]`)
	e2 := storyJSON(t, env, "", "api", "POST", "epics", "-F", "project="+pid, "-f", "subject=Encerrado "+suffix)
	var closedStatus any
	for _, st := range catalogList(t, env, "api", "GET", "epic-statuses", "--query", "project="+pid) {
		if st["is_closed"] == true {
			closedStatus = st["id"]
			break
		}
	}
	storyJSON(t, env, "", "api", "PATCH", fmt.Sprintf("epics/%v", e2["id"]), "-F", fmt.Sprintf("status=%v", closedStatus), "-F", fmt.Sprintf("version=%v", e2["version"]))
	story := storyJSON(t, svc, "", "story", "create", "--subject", "linked story")
	foreign := storyJSON(t, env, "", "api", "POST", "userstories", "-F", fmt.Sprintf("project=%v", other["id"]), "-f", "subject=foreign story")
	for _, id := range []any{story["id"], foreign["id"]} {
		storyJSON(t, env, "", "api", "POST", fmt.Sprintf("epics/%v/related_userstories", e1["id"]), "-F", fmt.Sprintf("epic=%v", e1["id"]), "-F", fmt.Sprintf("user_story=%v", id))
	}
	epics := catalogList(t, svc, "epic", "list")
	if fieldOf(epics, "ref") != fmt.Sprintf("%v,%v", e1["ref"], e2["ref"]) || epics[0]["url"] != fmt.Sprintf("%s/project/%s/epic/%v", testtaiga.URL(), slug, e1["ref"]) {
		t.Fatalf("epic list: %v", epics)
	}
	if open := catalogList(t, svc, "epic", "list", "--closed=false"); fieldOf(open, "ref") != fmt.Sprint(e1["ref"]) {
		t.Fatalf("epic list --closed=false: %s", fieldOf(open, "ref"))
	}
	if found := catalogList(t, svc, "epic", "list", "--search", "encerr"); fieldOf(found, "ref") != fmt.Sprint(e2["ref"]) {
		t.Fatalf("epic list --search: %s", fieldOf(found, "ref"))
	}
	got := storyJSON(t, svc, "", "epic", "get", fmt.Sprint(e1["ref"]))
	b, _ := json.Marshal(got["user_stories"])
	// svc cannot read the other project's story: it keeps only the id.
	want := fmt.Sprintf(`[{"id":%v,"project":%v,"ref":%v,"subject":"linked story"},{"id":%v}]`, story["id"], pid, story["ref"], foreign["id"])
	if string(b) != want || fmt.Sprint(got["tags"]) != "[cli]" {
		t.Fatalf("epic get: %s tags=%v\nwant %s", b, got["tags"], want)
	}
	asAdmin := storyJSON(t, env, "", "epic", "get", fmt.Sprint(e1["ref"]))
	b, _ = json.Marshal(asAdmin["user_stories"])
	if !strings.Contains(string(b), fmt.Sprintf(`"project_slug":"%v"`, other["slug"])) {
		t.Fatalf("admin epic get: %s", b)
	}
	_, errOut, code := runIn(t, svc, "", "epic", "get", fmt.Sprint(story["ref"]))
	if code != 5 || !strings.Contains(errOut, `"not_found"`) {
		t.Fatalf("epic get <story ref>: %d %s", code, errOut)
	}

	// Module off: refused by the CLI, though Taiga still serves the epics.
	storyJSON(t, env, "", "api", "PATCH", "projects/"+pid, "-F", "is_epics_activated=false")
	for _, args := range [][]string{{"epic", "list"}, {"epic", "get", fmt.Sprint(e1["ref"])}} {
		_, errOut, code := runIn(t, svc, "", args...)
		if code != 5 || !strings.Contains(errOut, "epics module is disabled") {
			t.Fatalf("%v with the module off: %d %s", args, code, errOut)
		}
	}
}
