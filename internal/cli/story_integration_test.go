//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func storyJSON(t *testing.T, env map[string]string, stdin string, args ...string) map[string]any {
	t.Helper()
	out, errOut, code := runIn(t, env, stdin, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d %s", args, code, errOut)
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return o
}

func storyRefs(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	out, errOut, code := runIn(t, env, "", append([]string{"story", "list"}, args...)...)
	if code != 0 {
		t.Fatalf("%v: exit %d %s", args, code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	for _, it := range items {
		refs = append(refs, fmt.Sprint(it["ref"]))
	}
	return strings.Join(refs, ",")
}

func TestIntegrationStoryLifecycle(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	project := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	pid := fmt.Sprint(project["id"])
	day := time.Now().Format("2006-01-02")
	sprint := "sprint " + suffix
	storyJSON(t, env, "", "api", "POST", "milestones", "-F", "project="+pid, "-f", "name="+sprint, "-f", "estimated_start="+day, "-f", "estimated_finish="+day)
	lane := "lane " + suffix
	storyJSON(t, env, "", "api", "POST", "swimlanes", "-F", "project="+pid, "-f", "name="+lane)

	tag := "alpha-" + suffix
	description := "integração \"stories\"\n\tcom controle \u0001 e acentos: ção\n"
	created := storyJSON(t, env, description, "story", "create", "--subject", "story "+suffix, "--tag", strings.ToUpper(tag), "--description-file", "-")
	ref := fmt.Sprint(created["ref"])
	if created["description"] != description || fmt.Sprint(created["tags"]) != "["+tag+"]" || created["version"] != float64(1) {
		t.Fatalf("created: %v", created)
	}
	if !strings.HasSuffix(fmt.Sprint(created["url"]), "/project/"+testtaiga.ProjectSlug+"/us/"+ref) {
		t.Fatalf("url %v", created["url"])
	}
	byID := storyJSON(t, env, "", "story", "get", "--id", fmt.Sprint(created["id"]))
	if byID["ref"] != created["ref"] {
		t.Fatalf("get --id: %v", byID)
	}
	if got := storyRefs(t, env, "--tag", tag, "--closed=false", "--status", "New", "--assignee", "me", "--search", suffix); got != "" {
		t.Fatalf("unassigned story matched --assignee me: %s", got)
	}
	if got := storyRefs(t, env, "--tag", tag, "--closed=false", "--status", "New", "--search", suffix, "--ref", ref); got != ref {
		t.Fatalf("list: %q", got)
	}

	update := []string{"story", "update", ref, "--add-tag", "beta", "--status", "In progress", "--milestone", sprint, "--swimlane", lane, "--append-description", "fim"}
	plan := storyJSON(t, env, "", append(update, "--dry-run")...)
	if plan["dry_run"] != true || storyJSON(t, env, "", "story", "get", ref)["version"] != created["version"] {
		t.Fatalf("dry-run changed the story: %v", plan)
	}
	updated := storyJSON(t, env, "", update...)
	if fmt.Sprint(updated["tags"]) != "["+tag+" beta]" || updated["milestone_name"] != sprint || updated["description"] != description+"\n\nfim" {
		t.Fatalf("updated: %v", updated)
	}
	if info, _ := updated["status_extra_info"].(map[string]any); info["name"] != "In progress" || updated["swimlane"] == nil {
		t.Fatalf("status/swimlane: %v %v", updated["status_extra_info"], updated["swimlane"])
	}

	if _, errOut, code := runIn(t, env, "", "story", "close", ref); code != 2 || !strings.Contains(errOut, "ambiguous_name") {
		t.Fatalf("default template has Done and Archived: %d %s", code, errOut)
	}
	before := storyJSON(t, env, "", "story", "get", ref)["version"]
	storyJSON(t, env, "", "story", "close", ref, "--status", "Done", "--dry-run")
	if v := storyJSON(t, env, "", "story", "get", ref)["version"]; v != before {
		t.Fatalf("close dry-run wrote: %v → %v", before, v)
	}
	closed := storyJSON(t, env, "", "story", "close", ref, "--status", "Done")
	if closed["is_closed"] != true {
		t.Fatalf("close: %v", closed)
	}
	again := storyJSON(t, env, "", "story", "close", ref)
	if again["version"] != closed["version"] {
		t.Fatal("closing a closed story wrote")
	}
	if got := storyRefs(t, env, "--tag", tag, "--closed"); got != ref {
		t.Fatalf("closed list: %q", got)
	}
	// An epic ref that is not an epic of the project is refused before any write.
	if _, errOut, code := runIn(t, env, "", "story", "update", ref, "--subject", "never", "--epic", "99999"); code != 5 || !strings.Contains(errOut, "not_found") {
		t.Fatalf("--epic: %d %s", code, errOut)
	}
	if st := storyJSON(t, env, "", "story", "get", ref); st["version"] != closed["version"] {
		t.Fatalf("--epic with an unknown epic wrote: version %v → %v", closed["version"], st["version"])
	}
}

// With the #244 session, story get works without TAIGA_TOKEN; a missing session is exit 3.
func TestIntegrationStoryGetWithSession(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	tokenEnv := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	created := storyJSON(t, tokenEnv, "", "story", "create", "--subject", "session probe")
	ref := fmt.Sprint(created["ref"])

	env := map[string]string{"HOME": t.TempDir(), "TAIGA_PROJECT": testtaiga.ProjectSlug}
	if _, errOut, code := runIn(t, env, testtaiga.AdminPassword+"\n", "auth", "login", "--url", testtaiga.URL(), "--username", testtaiga.AdminUser, "--password-stdin", "--insecure-storage"); code != 0 {
		t.Fatalf("login: %d %s", code, errOut)
	}
	if got := storyJSON(t, env, "", "story", "get", ref); got["id"] != created["id"] {
		t.Fatalf("get via session: %v", got)
	}
	if _, errOut, code := runIn(t, env, "", "auth", "logout"); code != 0 {
		t.Fatalf("logout: %d %s", code, errOut)
	}
	if _, errOut, code := runIn(t, env, "", "story", "get", ref); code != 3 {
		t.Fatalf("without session exit %d: %s", code, errOut)
	}
}

func apiList(t *testing.T, env map[string]string, args ...string) []map[string]any {
	t.Helper()
	out, errOut, code := runIn(t, env, "", append([]string{"api", "GET"}, args...)...)
	if code != 0 {
		t.Fatalf("%v: exit %d %s", args, code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

// assignProject returns env for a disposable project where admin and svc are both members
// (cli-test keeps svc out on purpose), creating the project and the membership when absent.
func assignProject(t *testing.T) (map[string]string, string, string) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	const slug = "cli-test-probe-assign"
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": slug}
	if _, _, code := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+slug); code == 5 {
		storyJSON(t, env, "", "api", "POST", "projects", "-f", "name="+slug, "-f", "description=taiga-cli probe")
	}
	project := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+slug)
	pid := fmt.Sprint(project["id"])
	users := map[string]string{}
	for _, u := range apiList(t, env, "users", "--query", "project="+pid) {
		users[fmt.Sprint(u["username"])] = fmt.Sprint(u["id"])
	}
	member := false
	for _, m := range apiList(t, env, "memberships", "--query", "project="+pid) {
		member = member || fmt.Sprint(m["user"]) == users[testtaiga.ServiceUser]
	}
	if !member {
		role := project["roles"].([]any)[0].(map[string]any)["id"]
		storyJSON(t, env, "", "api", "POST", "memberships", "-F", "project="+pid, "-F", fmt.Sprintf("role=%v", role), "-f", "username="+testtaiga.ServiceEmail)
	}
	return env, users[testtaiga.AdminUser], users[testtaiga.ServiceUser]
}

func TestIntegrationStoryAssigneesAndBlock(t *testing.T) {
	env, admin, svc := assignProject(t)
	users := func(o map[string]any) string {
		ids := []string{}
		for _, x := range o["assigned_users"].([]any) {
			ids = append(ids, fmt.Sprint(x))
		}
		sort.Strings(ids)
		return strings.Join(ids, ",")
	}
	both := strings.Join(func() []string { x := []string{admin, svc}; sort.Strings(x); return x }(), ",")

	created := storyJSON(t, env, "", "story", "create", "--subject", "assignees "+fmt.Sprint(time.Now().UnixNano()), "--assignee", "me")
	ref := fmt.Sprint(created["ref"])
	if users(created) != admin || created["assigned_to"] != nil {
		t.Fatalf("create: %v %v", created["assigned_users"], created["assigned_to"])
	}
	owned := storyJSON(t, env, "", "story", "create", "--subject", "owner "+fmt.Sprint(time.Now().UnixNano()), "--assignee", testtaiga.ServiceUser, "--owner-assignee", "me")
	if users(owned) != both || fmt.Sprint(owned["assigned_to"]) != admin {
		t.Fatalf("create owner: %v %v", owned["assigned_users"], owned["assigned_to"])
	}
	update := []string{"story", "update", ref, "--add-assignee", testtaiga.ServiceUser, "--owner-assignee", "me", "--block", "aguardando \"B6\"\nção"}
	plan := storyJSON(t, env, "", append(update, "--dry-run")...)
	if plan["dry_run"] != true || storyJSON(t, env, "", "story", "get", ref)["version"] != created["version"] {
		t.Fatalf("dry-run changed the story: %v", plan)
	}
	s := storyJSON(t, env, "", update...)
	if users(s) != both || fmt.Sprint(s["assigned_to"]) != admin || s["is_blocked"] != true || s["blocked_note"] != "aguardando \"B6\"\nção" {
		t.Fatalf("update: %v", s)
	}
	// The owner cannot leave the list alone: Taiga would keep showing them.
	if _, errOut, code := runIn(t, env, "", "story", "update", ref, "--remove-assignee", "me"); code != 2 || !strings.Contains(errOut, "main assignee") {
		t.Fatalf("remove owner: %d %s", code, errOut)
	}
	s = storyJSON(t, env, "", "story", "update", ref, "--remove-assignee", testtaiga.ServiceUser)
	if users(s) != admin || fmt.Sprint(s["assigned_to"]) != admin {
		t.Fatalf("remove svc: %v", s)
	}
	s = storyJSON(t, env, "", "story", "update", ref, "--remove-assignee", "me", "--clear-owner-assignee", "--unblock")
	if users(s) != "" || s["assigned_to"] != nil || s["is_blocked"] != false || s["blocked_note"] != "" {
		t.Fatalf("clear: %v", s)
	}
	again := storyJSON(t, env, "", "story", "update", ref, "--unblock", "--remove-assignee", testtaiga.ServiceUser)
	if again["version"] != s["version"] {
		t.Fatal("no-op wrote")
	}

	// An owner set only through assigned_to (never stored in the list) survives an owner change.
	raw := storyJSON(t, env, "", "api", "POST", "userstories", "-F", "project="+fmt.Sprint(created["project"]), "-f", "subject=implicit owner", "-F", "assigned_to="+admin)
	s = storyJSON(t, env, "", "story", "update", fmt.Sprint(raw["ref"]), "--owner-assignee", testtaiga.ServiceUser)
	if users(s) != both || fmt.Sprint(s["assigned_to"]) != svc {
		t.Fatalf("owner change dropped the previous owner: %v %v", s["assigned_users"], s["assigned_to"])
	}
}

// raceUpdate runs `story update` through a proxy that lets concurrent (a `taiga api PATCH` body,
// with the version read before) land between the CLI's reads and its first PATCH. It returns
// the exit code, how many PATCHes the CLI sent, and its stderr.
func raceUpdate(t *testing.T, env map[string]string, story map[string]any, concurrent []string, args ...string) (int, int, string) {
	t.Helper()
	return raceUpdateAnswer(t, env, story, concurrent, false, args...)
}

// raceUpdateAnswer is raceUpdate; with garble, the body of the CLI's successful PATCH answer is
// replaced by HTML (a proxy page), leaving the status as it was.
func raceUpdateAnswer(t *testing.T, env map[string]string, story map[string]any, concurrent []string, garble bool, args ...string) (int, int, string) {
	t.Helper()
	target, err := url.Parse(env["TAIGA_URL"])
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(r *http.Response) error {
		if garble && r.Request.Method == "PATCH" && r.StatusCode == 200 {
			r.Body = io.NopCloser(strings.NewReader("<html>proxy</html>"))
			r.ContentLength = -1
			r.Header.Del("Content-Length")
		}
		return nil
	}
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			patches++
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if patches == 1 {
				storyJSON(t, env, "", append([]string{"api", "PATCH", fmt.Sprintf("userstories/%v", story["id"]), "-F", fmt.Sprintf("version=%v", story["version"])}, concurrent...)...)
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer srv.Close()
	race := map[string]string{}
	for k, v := range env {
		race[k] = v
	}
	race["TAIGA_URL"] = srv.URL
	_, errOut, code := runIn(t, race, "", append([]string{"story", "update", fmt.Sprint(story["ref"])}, args...)...)
	return code, patches, errOut
}

// Taiga accepts a stale version when the PATCH sends none of the fields changed since; the CLI
// sends the whole block pair so that a concurrent block change is a conflict, not a silent loss.
func TestIntegrationStoryBlockRaces(t *testing.T) {
	env, _, _ := assignProject(t)
	pid := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+env["TAIGA_PROJECT"])["id"]
	for _, tc := range []struct {
		name       string
		args       []string
		concurrent []string
		blocked    bool
		note       string
	}{
		{"unblock keeps a concurrent note", []string{"--unblock"}, []string{"-f", "blocked_note=nota concorrente"}, true, "nota concorrente"},
		{"block does not succeed after a concurrent unblock", []string{"--block", "minha nota"}, []string{"-F", "is_blocked=false"}, false, ""},
	} {
		s := storyJSON(t, env, "", "api", "POST", "userstories", "-F", fmt.Sprintf("project=%v", pid), "-f", "subject=block race", "-F", "is_blocked=true")
		code, patches, errOut := raceUpdate(t, env, s, tc.concurrent, tc.args...)
		if !strings.Contains(errOut, "version_conflict") {
			t.Errorf("%s: %s", tc.name, errOut)
		}
		got := storyJSON(t, env, "", "story", "get", fmt.Sprint(s["ref"]))
		if code != 4 || patches != 1 || got["is_blocked"] != tc.blocked || got["blocked_note"] != tc.note {
			t.Errorf("%s: exit %d, %d PATCH, is_blocked=%v note=%q", tc.name, code, patches, got["is_blocked"], got["blocked_note"])
		}
	}
}

// Taiga's OCC never sees assigned_to (it is not in the story history), so a concurrent owner
// change cannot be refused by the server. The CLI checks the result after the write and fails
// with assignees_postcondition_failed instead of reporting success.
func TestIntegrationStoryAssigneeRaces(t *testing.T) {
	env, admin, svc := assignProject(t)
	pid := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+env["TAIGA_PROJECT"])["id"]
	for _, tc := range []struct {
		name    string
		initial []string
		args    []string
		owner   string // made the main assignee by someone else just before the CLI's PATCH
	}{
		{"removed user becomes owner", []string{"-F", "assigned_users=[" + svc + "]"}, []string{"--remove-assignee", testtaiga.ServiceUser}, svc},
		{"owner change drops a concurrent owner", []string{"-F", "assigned_users=[" + admin + "]"}, []string{"--owner-assignee", testtaiga.AdminUser}, svc},
		{"clear drops a concurrent owner", []string{"-F", "assigned_to=" + svc, "-F", "assigned_users=[" + svc + "]"}, []string{"--clear-owner-assignee"}, admin},
	} {
		s := storyJSON(t, env, "", append([]string{"api", "POST", "userstories", "-F", fmt.Sprintf("project=%v", pid), "-f", "subject=owner race"}, tc.initial...)...)
		code, patches, errOut := raceUpdate(t, env, s, []string{"-F", "assigned_to=" + tc.owner}, tc.args...)
		if code != 4 || patches != 1 || !strings.Contains(errOut, "assignees_postcondition_failed") || !strings.Contains(errOut, "do not re-run") {
			t.Errorf("%s: exit %d, %d PATCH: %s", tc.name, code, patches, errOut)
		}
	}
}

// With the PATCH answer unreadable, the version check falls back to the re-read after the write.
func TestIntegrationStoryAssigneeRaceUnreadableAnswer(t *testing.T) {
	env, admin, svc := assignProject(t)
	pid := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+env["TAIGA_PROJECT"])["id"]
	s := storyJSON(t, env, "", "api", "POST", "userstories", "-F", fmt.Sprintf("project=%v", pid), "-f", "subject=owner race, unreadable answer", "-F", "assigned_users=["+admin+"]")
	code, patches, errOut := raceUpdateAnswer(t, env, s, []string{"-F", "assigned_to=" + svc}, true, "--owner-assignee", testtaiga.AdminUser)
	if code != 4 || patches != 1 || !strings.Contains(errOut, "assignees_postcondition_failed") {
		t.Fatalf("exit %d, %d PATCH: %s", code, patches, errOut)
	}
}
