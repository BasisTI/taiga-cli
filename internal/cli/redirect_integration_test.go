//go:build integration

package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// redirectAfter is a proxy to the local Taiga that answers each request with method and path
// (under /api/v1/) with a 302 instead of Taiga's answer: after forwarding it to Taiga when apply,
// without forwarding it otherwise. The Location points to trap, which counts any request that
// follows it. sent counts the redirected requests.
func redirectAfter(t *testing.T, env map[string]string, trap, method, path string, apply bool) (map[string]string, *int) {
	t.Helper()
	sent := 0
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != method || r.URL.Path != "/api/v1/"+path {
			return false
		}
		sent++
		if apply {
			if got := forward(t, r, body); got.status >= 300 {
				t.Errorf("forward %s %s: %d %s", method, path, got.status, got.body)
			}
		}
		w.Header().Set("Location", trap+"/api/v1/"+path)
		w.WriteHeader(302)
		return true
	})
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["TAIGA_URL"] = url
	return out, &sent
}

// Every curated write against the real Taiga, answered with a redirect, applied and not applied
// (US #274): sent once, never exit 7, Location never followed; an update is decided by the
// re-read, a creation is never a success and names what it finds.
func TestIntegrationCuratedWritesRedirectIsUncertain(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	env, _, pid := freshProject(t, "cli-test-redirect-"+suffix)
	var followed atomic.Int32
	trapSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(418)
	}))
	t.Cleanup(trapSrv.Close)
	t.Cleanup(func() {
		if n := followed.Load(); n != 0 {
			t.Errorf("the Location was followed %d times", n)
		}
	})
	run := func(label, method, path string, apply bool, stdin string, args ...string) (string, string, int) {
		t.Helper()
		proxied, sent := redirectAfter(t, env, trapSrv.URL, method, path, apply)
		out, errOut, code := runIn(t, proxied, stdin, args...)
		if *sent != 1 || code == 7 {
			t.Errorf("%s (applied=%v): %s %s sent %d times, exit %d: %s", label, apply, method, path, *sent, code, errOut)
		}
		return out, errOut, code
	}
	expect := func(label string, apply bool, code int, errOut string, want int, wantText ...string) {
		t.Helper()
		if code != want {
			t.Errorf("%s (applied=%v): exit %d, want %d: %s", label, apply, code, want, errOut)
			return
		}
		for _, w := range wantText {
			if !strings.Contains(errOut, w) {
				t.Errorf("%s (applied=%v): %q missing: %s", label, apply, w, errOut)
			}
		}
	}

	storyJSON(t, env, "", "api", "PATCH", "projects/"+pid, "-F", "is_epics_activated=true")
	epic := storyJSON(t, env, "", "api", "POST", "epics", "-F", "project="+pid, "-f", "subject=redirect epic")
	story := storyJSON(t, env, "", "story", "create", "--subject", "redirect "+suffix)
	ref, path := fmt.Sprint(story["ref"]), fmt.Sprintf("userstories/%v", story["id"])
	get := func() map[string]any { return storyJSON(t, env, "", "story", "get", ref) }

	// Story PATCH: the re-read decides.
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story update", "PATCH", path, apply, "", "story", "update", ref, "--subject", fmt.Sprintf("subject %v", apply), "--append-description", fmt.Sprintf("line %v", apply))
		if apply {
			expect("story update", apply, code, errOut, 0)
		} else {
			expect("story update", apply, code, errOut, 1, "story_update_unconfirmed", "taiga story get "+ref)
		}
	}
	if s := get(); s["subject"] != "subject true" || s["description"] != "line true" {
		t.Errorf("story after update: %v %q", s["subject"], s["description"])
	}
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story block", "PATCH", path, apply, "", "story", "update", ref, "--block", "esperando")
		expect("story block", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "story_update_unconfirmed"}[apply])
	}
	storyJSON(t, env, "", "api", "POST", "swimlanes", "-F", "project="+pid, "-f", "name=A") // takes every story
	storyJSON(t, env, "", "api", "POST", "swimlanes", "-F", "project="+pid, "-f", "name=B")
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story swimlane", "PATCH", path, apply, "", "story", "update", ref, "--swimlane", "B")
		expect("story swimlane", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "story_update_unconfirmed"}[apply])
	}
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story assignees", "PATCH", path, apply, "", "story", "update", ref, "--add-assignee", testtaiga.ServiceUser)
		expect("story assignees", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "story_update_unconfirmed"}[apply])
	}
	if s := get(); s["is_blocked"] != true || len(s["assigned_users"].([]any)) != 1 {
		t.Errorf("story after block/assignees: %v %v", s["is_blocked"], s["assigned_users"])
	}
	storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", "Notas", "--type", "text")
	values := fmt.Sprintf("userstories/custom-attributes-values/%v", story["id"])
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story field set", "PATCH", values, apply, "", "story", "field", "set", ref, fmt.Sprintf("Notas=%v", apply))
		expect("story field set", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "story_update_unconfirmed"}[apply])
	}
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story close", "PATCH", path, apply, "", "story", "close", ref, "--status", "Done")
		expect("story close", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "story_update_unconfirmed"}[apply])
	}
	if s := get(); s["is_closed"] != true || fmt.Sprint(s["swimlane"]) == "<nil>" {
		t.Errorf("story after close: %v %v", s["is_closed"], s["swimlane"])
	}

	// Story comment: never a success (option B); the new comment is named when it landed.
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("story comment", "PATCH", path, apply, "", "story", "comment", ref, "--body", "redirected "+suffix)
		expect("story comment", apply, code, errOut, 1, "comment_unconfirmed", map[bool]string{true: "1 new comment(s)", false: "not in the history"}[apply])
	}
	if n := len(comments(t, env, ref)); n != 1 {
		t.Errorf("story comments: %d", n)
	}

	// Story create, with and without --epic: never a success; the story found is named and
	// never linked.
	for _, epicFlag := range [][]string{nil, {"--epic", fmt.Sprint(epic["ref"])}} {
		for _, apply := range []bool{false, true} {
			subject := fmt.Sprintf("created %v %d %s", apply, len(epicFlag), suffix)
			out, errOut, code := run("story create", "POST", "userstories", apply, "", append([]string{"story", "create", "--subject", subject}, epicFlag...)...)
			found := storyRefs(t, env, "--search", subject)
			if !apply {
				expect("story create", apply, code, errOut, 1, "story_create_unconfirmed", "no new story")
				if found != "" {
					t.Errorf("story create not applied: created %q", found)
				}
				continue
			}
			expect("story create", apply, code, errOut, 1, "story_create_unconfirmed", "#"+found+" (id ", "taiga story get "+found)
			if out != "" || found == "" || strings.Contains(found, ",") || storyEpicRefs(t, env, found) != "" {
				t.Errorf("story create applied: %q %q", out, found)
			}
		}
	}

	// Epic link: idempotent, decided by the re-read.
	linked := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "link "+suffix)["ref"])
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("epic link", "POST", fmt.Sprintf("epics/%v/related_userstories", epic["id"]), apply, "", "epic", "link", fmt.Sprint(epic["ref"]), linked)
		expect("epic link", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "epic_link_unconfirmed"}[apply])
	}
	if got := storyEpicRefs(t, env, linked); got != fmt.Sprint(epic["ref"]) {
		t.Errorf("epic link: %q", got)
	}

	// Field create: never a success; Taiga refuses a second one with the name, so a re-run
	// returns the one there.
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("field create", "POST", "userstory-custom-attributes", apply, "", "field", "create", "--kind", "story", "--name", "Redir", "--type", "text")
		expect("field create", apply, code, errOut, 1, "field_create_unconfirmed", map[bool]string{true: "has a field with this name", false: "no field with this name"}[apply])
	}
	if again := storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", "Redir", "--type", "text"); again["name"] != "Redir" || countNamed(t, env, "story", "Redir") != 1 {
		t.Errorf("field create re-run: %v", again)
	}

	// Attachment upload: never a success; the new one is named.
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("attachment upload", "POST", "userstories/attachments", apply, "", "attachment", "upload", ref, localFile(t, fmt.Sprintf("r%v.txt", apply), []byte("redirect")))
		expect("attachment upload", apply, code, errOut, 1, "attachment_unconfirmed", map[bool]string{true: "(id ", false: "does not show it"}[apply])
	}
	if n := len(attachmentList(t, env, ref)); n != 1 {
		t.Errorf("attachments: %d", n)
	}

	// Project apply: a status creation is never a success; a re-run converges. The reorder is
	// decided by the re-read.
	const statusTOML = "[[story_status]]\nname = \"Redir\"\ncolor = \"#000000\"\n"
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("status create", "POST", "userstory-statuses", apply, statusTOML, "project", "apply", "-f", "-")
		expect("status create", apply, code, errOut, 1, "status_create_unconfirmed", map[bool]string{true: "has a status with this name", false: "no status with this name"}[apply])
	}
	if out, errOut, code := runIn(t, env, statusTOML, "project", "apply", "-f", "-"); code != 0 || applyResult(t, out)["complete"] != true {
		t.Errorf("status re-run: %d %s", code, errOut)
	}
	const orderTOML = "[[story_status]]\nname = \"Redir\"\ncolor = \"#000000\"\nafter = \"New\"\n"
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("reorder", "POST", "userstory-statuses/bulk_update_order", apply, orderTOML, "project", "apply", "-f", "-")
		expect("reorder", apply, code, errOut, map[bool]int{true: 0, false: 4}[apply], map[bool]string{true: "", false: "status_order_postcondition_failed"}[apply])
	}
	if got := statusOrder(t, env); !strings.HasPrefix(got, "New,Redir,") {
		t.Errorf("order: %s", got)
	}

	// Tasks: the update is decided by the re-read, the creation and the comment never succeed.
	open := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "tasks "+suffix)["ref"])
	task := storyJSON(t, env, "", "task", "create", "--story", open, "--subject", "task "+suffix)
	tref, tpath := fmt.Sprint(task["ref"]), fmt.Sprintf("tasks/%v", task["id"])
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("task update", "PATCH", tpath, apply, "", "task", "update", tref, "--append-description", fmt.Sprintf("line %v", apply))
		expect("task update", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "task_update_unconfirmed"}[apply])
	}
	storyJSON(t, env, "", "field", "create", "--kind", "task", "--name", "Horas", "--type", "text")
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("task field set", "PATCH", fmt.Sprintf("tasks/custom-attributes-values/%v", task["id"]), apply, "", "task", "field", "set", tref, fmt.Sprintf("Horas=%v", apply))
		expect("task field set", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "task_update_unconfirmed"}[apply])
	}
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("task comment", "PATCH", tpath, apply, "", "task", "comment", tref, "--body", "redirected "+suffix)
		expect("task comment", apply, code, errOut, 1, "comment_unconfirmed", map[bool]string{true: "1 new comment(s)", false: "not in the history"}[apply])
	}
	for _, apply := range []bool{false, true} {
		_, errOut, code := run("task close", "PATCH", tpath, apply, "", "task", "close", tref)
		expect("task close", apply, code, errOut, map[bool]int{true: 0, false: 1}[apply], map[bool]string{true: "", false: "task_update_unconfirmed"}[apply])
	}
	if got := storyJSON(t, env, "", "task", "get", tref); got["description"] != "line true" || got["is_closed"] != true {
		t.Errorf("task: %q %v", got["description"], got["is_closed"])
	}
	for _, apply := range []bool{false, true} {
		subject := fmt.Sprintf("new task %v %s", apply, suffix)
		_, errOut, code := run("task create", "POST", "tasks", apply, "", "task", "create", "--story", open, "--subject", subject)
		expect("task create", apply, code, errOut, 1, "task_create_unconfirmed", map[bool]string{true: "taiga task get ", false: "no new task"}[apply])
	}
	if got := taskRefs(t, env, "--story", open); strings.Count(got, ",") != 1 {
		t.Errorf("tasks of the story: %q", got)
	}
}
