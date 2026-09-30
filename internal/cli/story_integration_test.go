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
	if _, errOut, code := runIn(t, env, "", "story", "update", ref, "--epic", "1"); code != 2 || !strings.Contains(errOut, "unsupported_operation") {
		t.Fatalf("--epic: %d %s", code, errOut)
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
