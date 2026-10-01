//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// commentProject returns env for a disposable project where svc is a member and Taiga's GitLab
// integration is on with a test secret, creating what is missing.
func commentProject(t *testing.T) (map[string]string, string, string) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	const slug = "cli-test-probe-comments"
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": slug}
	if _, _, code := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+slug); code == 5 {
		storyJSON(t, env, "", "api", "POST", "projects", "-f", "name="+slug, "-f", "description=taiga-cli probe")
	}
	project := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+slug)
	pid := fmt.Sprint(project["id"])
	svc := ""
	for _, u := range apiList(t, env, "users", "--query", "project="+pid) {
		if u["username"] == testtaiga.ServiceUser {
			svc = fmt.Sprint(u["id"])
		}
	}
	member := false
	for _, m := range apiList(t, env, "memberships", "--query", "project="+pid) {
		member = member || fmt.Sprint(m["user"]) == svc
	}
	if !member {
		role := project["roles"].([]any)[0].(map[string]any)["id"]
		storyJSON(t, env, "", "api", "POST", "memberships", "-F", "project="+pid, "-F", fmt.Sprintf("role=%v", role), "-f", "username="+testtaiga.ServiceEmail)
	}
	const secret = "cli-test-gitlab-secret"
	direct(t, token, "PATCH", "/api/v1/projects/"+pid+"/modules", map[string]any{"gitlab": map[string]any{"secret": secret, "valid_origin_ips": []string{}}}, 204)
	return env, pid, secret
}

func direct(t *testing.T, token, method, path string, body any, status int) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, testtaiga.URL()+path, bytes.NewReader(payload))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
}

func comments(t *testing.T, env map[string]string, args ...string) []map[string]any {
	t.Helper()
	out, errOut, code := runIn(t, env, "", append([]string{"story", "comments"}, args...)...)
	if code != 0 {
		t.Fatalf("comments %v: %d %s", args, code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestIntegrationStoryComments(t *testing.T) {
	env, pid, secret := commentProject(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	story := storyJSON(t, env, "fica igual", "story", "create", "--subject", "comments "+suffix, "--description-file", "-")
	ref := fmt.Sprint(story["ref"])
	if items := comments(t, env, ref); len(items) != 0 {
		t.Fatalf("new story has comments: %v", items)
	}

	text := "Decisão " + suffix + ": \"aspas\" e acentuação\n\n- item **md**\n\tfim"
	plan := storyJSON(t, env, "", "story", "comment", ref, "--body", text, "--dry-run")
	if plan["dry_run"] != true || len(comments(t, env, ref)) != 0 {
		t.Fatalf("dry-run wrote: %v", plan)
	}
	written := storyJSON(t, env, text, "story", "comment", ref, "--body-file", "-")
	if written["description"] != "fica igual" || written["version"] != story["version"].(float64)+1 {
		t.Fatalf("comment changed the story: %v", written)
	}
	items := comments(t, env, ref)
	if len(items) != 1 || items[0]["comment"] != text || items[0]["is_system"] != false || items[0]["story_ref"] != story["ref"] {
		t.Fatalf("published: %v", items)
	}
	if user := items[0]["user"].(map[string]any); user["username"] != testtaiga.AdminUser {
		t.Fatalf("author: %v", user)
	}

	// A service account comment is human text: always listed.
	svcToken, _ := testtaiga.Login(t, testtaiga.ServiceUser, testtaiga.ServicePassword)
	svcEnv := map[string]string{"TAIGA_URL": env["TAIGA_URL"], "TAIGA_TOKEN": svcToken, "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	storyJSON(t, svcEnv, "", "story", "comment", ref, "--body", "registrado pela conta de integração")

	// Taiga's GitLab integration writes as its system user for a push that mentions the story.
	hook := fmt.Sprintf("/api/v1/gitlab-hook?project=%s&key=%s", pid, secret)
	direct(t, "", "POST", hook, map[string]any{"object_kind": "push", "commits": []any{map[string]any{
		"id": "abc123", "url": "https://gitlab.example/g/p/-/commit/abc123", "message": "Ajustar parser TG-" + ref, "author": map[string]any{"name": "Dev Exemplo"}}}}, 204)

	visible := comments(t, env, ref)
	all := comments(t, env, ref, "--include-system")
	if len(visible) != 2 || len(all) != 3 {
		t.Fatalf("visible %d, all %d: %v", len(visible), len(all), all)
	}
	if all[0]["is_system"] != true || !strings.HasPrefix(fmt.Sprint(all[0]["comment"]), "This user story has been mentioned by Dev Exemplo") ||
		visible[0]["comment"] != "registrado pela conta de integração" || visible[1]["comment"] != text {
		t.Fatalf("order or filter: %v", all)
	}
}

// History is paginated by 30 on the server; the listing must carry every comment.
func TestIntegrationStoryCommentsPaginate(t *testing.T) {
	env, _, _ := commentProject(t)
	story := storyJSON(t, env, "", "story", "create", "--subject", fmt.Sprint("many comments ", time.Now().UnixNano()))
	path := fmt.Sprintf("/api/v1/userstories/%v", story["id"])
	for i := 0; i < 32; i++ {
		// Taiga accepts the story's first version for every comment (docs/api-notes.md).
		direct(t, env["TAIGA_TOKEN"], "PATCH", path, map[string]any{"comment": fmt.Sprint("n", i), "version": story["version"]}, 200)
	}
	items := comments(t, env, fmt.Sprint(story["ref"]))
	if len(items) != 32 || items[0]["comment"] != "n31" || items[31]["comment"] != "n0" {
		t.Fatalf("%d comments: first %v", len(items), items[0]["comment"])
	}
}
