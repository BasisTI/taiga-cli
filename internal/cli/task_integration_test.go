//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

	// POST applied, answer lost: the task is found by its fields, never created twice, with a warning.
	out, errOut, code := runIn(t, dropAnswer(t, env, "POST", "tasks", true), "", "task", "create", "--story", story, "--subject", "lost "+suffix, "--tag", "a", "--due-date", "2026-12-31")
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil || code != 0 || !strings.Contains(errOut, "warning [task_create_matched]") {
		t.Fatalf("applied-lost: %d %s %s", code, out, errOut)
	}
	if got := taskRefs(t, env, "--story", story); got != fmt.Sprint(created["ref"]) {
		t.Fatalf("applied-lost: %q, created %v", got, created["ref"])
	}
	// POST not applied, answer lost: unconfirmed, exit 1, nothing created.
	_, errOut, code = runIn(t, dropAnswer(t, env, "POST", "tasks", false), "", "task", "create", "--story", story, "--subject", "never "+suffix)
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

// truncateAnswer is a proxy to the local Taiga that forwards everything and cuts the answer of
// each PATCH to path after a few bytes (the connection drops mid-body after the 2xx).
func truncateAnswer(t *testing.T, env map[string]string, path string) (map[string]string, *int) {
	t.Helper()
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req, _ := http.NewRequest(r.Method, testtaiga.URL()+r.URL.RequestURI(), bytes.NewReader(body))
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("forward: %v", err)
			w.WriteHeader(502)
			return
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprint(len(answer)))
		w.WriteHeader(resp.StatusCode)
		if r.Method == "PATCH" && r.URL.Path == "/api/v1/"+path {
			patches++
			_, _ = w.Write(answer[:10])
			return
		}
		_, _ = w.Write(answer)
	}))
	t.Cleanup(srv.Close)
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["TAIGA_URL"] = srv.URL
	return out, &patches
}

func TestIntegrationTaskFieldsAndComments(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, svc, _ := freshProject(t, "cli-test-tasks-fields-"+suffix)
	story := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "story")["ref"])
	task := storyJSON(t, env, "", "task", "create", "--story", story, "--subject", "task "+suffix)
	ref := fmt.Sprint(task["ref"])
	hours := storyJSON(t, env, "", "field", "create", "--kind", "task", "--name", "Horas", "--type", "text")
	due := storyJSON(t, env, "", "field", "create", "--kind", "task", "--name", "Prazo", "--type", "date")

	set := storyJSON(t, env, "", "task", "field", "set", ref, "Horas=8h", "Prazo=2026-12-31")
	got := set["attributes_values"].(map[string]any)
	if got[fmt.Sprint(hours["id"])] != "8h" || got[fmt.Sprint(due["id"])] != "2026-12-31" || set["version"] != float64(2) || !strings.HasSuffix(fmt.Sprint(set["url"]), "/task/"+ref) {
		t.Fatalf("set: %v", set)
	}
	cleared := storyJSON(t, env, "", "task", "field", "set", ref, "--unset", "Prazo")
	if got := cleared["attributes_values"].(map[string]any); got[fmt.Sprint(due["id"])] != nil || got[fmt.Sprint(hours["id"])] != "8h" {
		t.Fatalf("unset: %v", cleared)
	}
	if listed := storyJSON(t, env, "", "task", "field", "list", ref); listed["version"] != cleared["version"] {
		t.Fatalf("list: %v", listed)
	}
	// The answer of the values PATCH is cut after the 2xx: the re-read stands in.
	proxied, patches := truncateAnswer(t, env, fmt.Sprintf("tasks/custom-attributes-values/%v", task["id"]))
	truncated := storyJSON(t, proxied, "", "task", "field", "set", ref, "Horas=9h")
	if got := truncated["attributes_values"].(map[string]any); got[fmt.Sprint(hours["id"])] != "9h" || *patches != 1 {
		t.Fatalf("truncated: %v, %d PATCH", truncated, *patches)
	}

	// Comments: svc and admin, newest first, on history/task.
	storyJSON(t, svc, "", "task", "comment", ref, "--body", "primeiro "+suffix)
	storyJSON(t, env, "body: \"x\"\nção\n", "task", "comment", ref, "--body-file", "-")
	out, errOut, code := runIn(t, env, "", "task", "comments", ref)
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || code != 0 || len(items) != 2 {
		t.Fatalf("comments: %d %s %s", code, out, errOut)
	}
	if items[0]["comment"] != "body: \"x\"\nção\n" || items[1]["comment"] != "primeiro "+suffix || fmt.Sprint(items[0]["task_ref"]) != ref {
		t.Fatalf("comments: %v", items)
	}
	if author, _ := items[1]["user"].(map[string]any); author["username"] != testtaiga.ServiceUser {
		t.Fatalf("author: %v", items[1]["user"])
	}
}

// otherProcess is a proxy to the local Taiga that, for the first POST tasks, does not forward
// it: it creates instead, with the same token, the task body (another process of the same
// account), and answers status to the CLI.
func otherProcess(t *testing.T, env map[string]string, status int, body map[string]any) map[string]string {
	t.Helper()
	done := false
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if done || r.Method != "POST" || r.URL.Path != "/api/v1/tasks" {
			return false
		}
		done = true
		payload, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", testtaiga.URL()+"/api/v1/tasks", bytes.NewReader(payload))
		req.Header = r.Header.Clone()
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 201 {
			t.Errorf("other process: %v %v", err, resp)
		} else {
			_ = resp.Body.Close()
		}
		w.WriteHeader(status)
		return true
	})
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["TAIGA_URL"] = url
	return out
}

// A lost POST while another process of the same account creates a task with the same subject:
// a candidate with other fields is shown, never adopted; one with every field equal is taken,
// with the warning that the match is not a proof.
func TestIntegrationTaskCreateLostNextToAnotherProcess(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, _, pid := freshProject(t, "cli-test-tasks-other-"+suffix)
	s := storyJSON(t, env, "", "story", "create", "--subject", "story")
	story := fmt.Sprint(s["ref"])
	for _, status := range []int{502, 302} {
		subject := fmt.Sprintf("Escrever testes %d %s", status, suffix)
		theirs := map[string]any{"project": pid, "user_story": s["id"], "subject": subject, "description": "created by process B", "tags": []string{"process-b"}}
		_, errOut, code := runIn(t, otherProcess(t, env, status, theirs), "requested by process A", "task", "create", "--story", story,
			"--subject", subject, "--description-file", "-", "--tag", "process-a", "--due-date", "2026-12-31")
		if code != 1 || !strings.Contains(errOut, "task_create_unconfirmed") || !strings.Contains(errOut, "differ from the request") || strings.Contains(errOut, "warning") {
			t.Fatalf("%d, other fields: %d %s", status, code, errOut)
		}
		same := map[string]any{"project": pid, "user_story": s["id"], "subject": subject + " same", "description": "same", "tags": []string{"x"}, "due_date": "2026-12-31"}
		out, errOut, code := runIn(t, otherProcess(t, env, status, same), "same", "task", "create", "--story", story,
			"--subject", subject+" same", "--description-file", "-", "--tag", "X", "--due-date", "2026-12-31")
		if code != 0 || !strings.Contains(out, "same") || !strings.Contains(errOut, "warning [task_create_matched]") {
			t.Fatalf("%d, same fields: %d %s %s", status, code, out, errOut)
		}
	}
}
