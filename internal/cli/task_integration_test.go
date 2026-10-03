//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func taskRefs(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	out, errOut, code := runIn(t, env, "", append([]string{"task", "list"}, args...)...)
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

func TestIntegrationTaskLifecycle(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, svc, _ := freshProject(t, "cli-test-tasks-"+suffix)
	s1 := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "story one")["ref"])
	s2 := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "story two")["ref"])
	due := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	description := "tarefa \"x\"\n\tcom acentos: ção\n"

	create := []string{"task", "create", "--story", s1, "--subject", "task " + suffix, "--tag", "Alpha", "--assignee", testtaiga.ServiceUser,
		"--due-date", due, "--status", "In progress", "--description-file", "-"}
	plan := storyJSON(t, env, description, append(create, "--dry-run")...)
	if plan["dry_run"] != true || taskRefs(t, env) != "" {
		t.Fatalf("dry-run created a task: %v", plan)
	}
	created := storyJSON(t, env, description, create...)
	ref := fmt.Sprint(created["ref"])
	info, _ := created["status_extra_info"].(map[string]any)
	if created["description"] != description || fmt.Sprint(created["tags"]) != "[alpha]" || created["version"] != float64(1) || created["due_date"] != due ||
		info["name"] != "In progress" || !strings.HasSuffix(fmt.Sprint(created["url"]), "/task/"+ref) {
		t.Fatalf("created: %v", created)
	}
	other := fmt.Sprint(storyJSON(t, env, "", "task", "create", "--story", s2, "--subject", "other "+suffix)["ref"])

	if got := taskRefs(t, env, "--story", s1); got != ref {
		t.Fatalf("--story: %q", got)
	}
	for _, args := range [][]string{{"--assignee", testtaiga.ServiceUser}, {"--status", "In progress"}, {"--tag", "ALPHA"}, {"--search", "TASK " + suffix}} {
		if got := taskRefs(t, env, args...); got != ref {
			t.Fatalf("%v: %q", args, got)
		}
	}
	if got := taskRefs(t, env, "--closed=false", "--story", s2); got != other {
		t.Fatalf("--closed=false: %q", got)
	}
	if byID := storyJSON(t, env, "", "task", "get", "--id", fmt.Sprint(created["id"])); byID["ref"] != created["ref"] {
		t.Fatalf("get --id: %v", byID)
	}
	if _, errOut, code := runIn(t, env, "", "task", "get", s1); code != 5 {
		t.Fatalf("a story ref is not a task: %d %s", code, errOut)
	}

	updated := storyJSON(t, env, "", "task", "update", ref, "--append-description", "fim", "--add-tag", "beta", "--block", "esperando o B6", "--clear-due-date")
	if updated["description"] != description+"\n\nfim" || fmt.Sprint(updated["tags"]) != "[alpha beta]" || updated["is_blocked"] != true ||
		updated["blocked_note"] != "esperando o B6" || updated["due_date"] != nil {
		t.Fatalf("updated: %v", updated)
	}
	// svc, a member, changes the task too; the CLI writes on top of the version it reads.
	storyJSON(t, svc, "", "task", "update", ref, "--assignee", "me")
	updated = storyJSON(t, env, "", "task", "update", ref, "--unblock", "--clear-assignee")
	if updated["is_blocked"] != false || updated["blocked_note"] != "" || updated["assigned_to"] != nil {
		t.Fatalf("unblock/clear: %v", updated)
	}
	before := updated["version"]
	if again := storyJSON(t, env, "", "task", "update", ref, "--unblock", "--clear-assignee"); again["version"] != before {
		t.Fatal("a no-op update wrote")
	}

	storyJSON(t, env, "", "task", "close", ref, "--dry-run")
	if v := storyJSON(t, env, "", "task", "get", ref)["version"]; v != before {
		t.Fatalf("close dry-run wrote: %v → %v", before, v)
	}
	closed := storyJSON(t, env, "", "task", "close", ref)
	if closed["is_closed"] != true || fmt.Sprint(closed["tags"]) != "[alpha beta]" {
		t.Fatalf("close: %v", closed)
	}
	if again := storyJSON(t, env, "", "task", "close", ref); again["version"] != closed["version"] {
		t.Fatal("closing a closed task wrote")
	}
	if got := taskRefs(t, env, "--closed"); got != ref {
		t.Fatalf("--closed: %q", got)
	}
}

// dropAnswer is a proxy to the local Taiga that, for the first request with method and path,
// forwards it (when forward) and then answers 502 instead of the real answer.
func dropAnswer(t *testing.T, env map[string]string, method, path string, forward bool) map[string]string {
	t.Helper()
	dropped := false
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if dropped || r.Method != method || r.URL.Path != "/api/v1/"+path {
			return false
		}
		dropped = true
		if forward {
			req, _ := http.NewRequest(r.Method, testtaiga.URL()+r.URL.RequestURI(), bytes.NewReader(body))
			req.Header = r.Header.Clone()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("forward: %v", err)
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}
		w.WriteHeader(502)
		return true
	})
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["TAIGA_URL"] = url
	return out
}

func TestIntegrationTaskLostAnswers(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, _, _ := freshProject(t, "cli-test-tasks-lost-"+suffix)
	story := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "story")["ref"])

	// POST applied, answer lost: the task is found, never created twice.
	created := storyJSON(t, dropAnswer(t, env, "POST", "tasks", true), "", "task", "create", "--story", story, "--subject", "lost "+suffix)
	if got := taskRefs(t, env, "--story", story); got != fmt.Sprint(created["ref"]) {
		t.Fatalf("applied-lost: %q, created %v", got, created["ref"])
	}
	// POST not applied, answer lost: unconfirmed, exit 1, nothing created.
	_, errOut, code := runIn(t, dropAnswer(t, env, "POST", "tasks", false), "", "task", "create", "--story", story, "--subject", "never "+suffix)
	if code != 1 || !strings.Contains(errOut, "task_create_unconfirmed") || taskRefs(t, env, "--story", story) != fmt.Sprint(created["ref"]) {
		t.Fatalf("not-applied-lost: %d %s", code, errOut)
	}

	// PATCH applied, answer lost: the re-read confirms it; the description is appended once.
	ref := fmt.Sprint(created["ref"])
	path := fmt.Sprintf("tasks/%v", created["id"])
	updated := storyJSON(t, dropAnswer(t, env, "PATCH", path, true), "", "task", "update", ref, "--append-description", "uma vez")
	if updated["description"] != "uma vez" {
		t.Fatalf("applied-lost update: %v", updated["description"])
	}
	// PATCH not applied, answer lost: unconfirmed, exit 1.
	_, errOut, code = runIn(t, dropAnswer(t, env, "PATCH", path, false), "", "task", "update", ref, "--append-description", "nunca")
	if code != 1 || !strings.Contains(errOut, "task_update_unconfirmed") {
		t.Fatalf("not-applied-lost update: %d %s", code, errOut)
	}
	if got := storyJSON(t, env, "", "task", "get", ref)["description"]; got != "uma vez" {
		t.Fatalf("description: %q", got)
	}
}
