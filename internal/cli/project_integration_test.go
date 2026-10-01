//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// freshProject creates a project for this run, with svc as a plain member, and returns the
// admin env, the svc env and its id.
func freshProject(t *testing.T, name string) (map[string]string, map[string]string, string) {
	t.Helper()
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token}
	project := storyJSON(t, env, "", "api", "POST", "projects", "-f", "name="+name, "-f", "description=taiga-cli project apply")
	pid := fmt.Sprint(project["id"])
	env["TAIGA_PROJECT"] = fmt.Sprint(project["slug"])
	role := project["roles"].([]any)[0].(map[string]any)["id"]
	storyJSON(t, env, "", "api", "POST", "memberships", "-F", "project="+pid, "-F", fmt.Sprintf("role=%v", role), "-f", "username="+testtaiga.ServiceEmail)
	svcToken, _ := testtaiga.Login(t, testtaiga.ServiceUser, testtaiga.ServicePassword)
	svc := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": svcToken, "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	return env, svc, pid
}

func statusNames(t *testing.T, env map[string]string) string {
	t.Helper()
	out, errOut, code := runIn(t, env, "", "status", "list")
	if code != 0 {
		t.Fatalf("status list: %d %s", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, it := range items {
		names = append(names, fmt.Sprintf("%v/%v/%v", it["name"], it["color"], it["is_closed"]))
	}
	return strings.Join(names, ",")
}

func fieldNames(t *testing.T, env map[string]string) string {
	t.Helper()
	out, errOut, code := runIn(t, env, "", "field", "list", "--kind", "story")
	if code != 0 {
		t.Fatalf("field list: %d %s", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, it := range items {
		names = append(names, fmt.Sprintf("%v/%v", it["name"], it["type"]))
	}
	return strings.Join(names, ",")
}

func applyResult(t *testing.T, out string) map[string]any {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return r
}

const applyTOML = `[[story_status]]
name = "In revision"
color = "#5178D3"

[[story_status]]
name = "Waiting for deployment"
color = "#40A8E4"
after = "In revision"

[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste"

[[story_field]]
name = "Executor"
type = "text"
`

func TestIntegrationProjectApply(t *testing.T) {
	env, svc, pid := freshProject(t, "cli-test-apply-"+fmt.Sprint(time.Now().UnixNano()))
	// An extra status the file does not declare.
	storyJSON(t, env, "", "api", "POST", "userstory-statuses", "-F", "project="+pid, "-f", "name=Extra", "-f", "color=#123456", "-F", "order=7")
	before, beforeFields := statusNames(t, env), fieldNames(t, env)

	// Without admin_project_values: exit 6 and nothing written, dry-run included.
	for _, args := range [][]string{{"project", "apply", "-f", "-"}, {"project", "apply", "-f", "-", "--dry-run"}} {
		if _, errOut, code := runIn(t, svc, applyTOML, args...); code != 6 || !strings.Contains(errOut, "forbidden") {
			t.Fatalf("svc %v: %d %s", args, code, errOut)
		}
	}
	// svc can still plan: it only reads.
	if _, errOut, code := runIn(t, svc, applyTOML, "project", "plan", "-f", "-"); code != 0 {
		t.Fatalf("svc plan: %d %s", code, errOut)
	}
	// Dry-run, reorder, drift and cycles write nothing.
	if out, errOut, code := runIn(t, env, applyTOML, "project", "apply", "-f", "-", "--dry-run"); code != 0 || len(applyResult(t, out)["requests"].([]any)) != 4 {
		t.Fatalf("dry-run: %d %s %s", code, errOut, out)
	}
	for toml, want := range map[string]string{
		"[[story_status]]\nname = \"Done\"\ncolor = \"#000000\"\nclosed = true\n":                                                                  "definition_drift",
		"[[story_status]]\nname = \"in progress\"\ncolor = \"#E47C40\"\n":                                                                          "definition_drift",
		"[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nafter = \"B\"\n[[story_status]]\nname = \"B\"\ncolor = \"#000000\"\nafter = \"A\"\n": "cycle",
	} {
		if _, errOut, code := runIn(t, env, toml, "project", "apply", "-f", "-"); code != 2 || !strings.Contains(errOut, want) {
			t.Fatalf("%q: %d %s", toml, code, errOut)
		}
	}
	if statusNames(t, env) != before || fieldNames(t, env) != beforeFields {
		t.Fatal("a refused apply changed the project")
	}

	// Apply creates the statuses at the end in file order, and the fields.
	out, errOut, code := runIn(t, env, applyTOML, "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 0 || r["complete"] != true || len(r["applied"].([]any)) != 4 || len(r["remaining"].([]any)) != 0 {
		t.Fatalf("apply: %d %s %s", code, errOut, out)
	}
	want := before + ",In revision/#5178D3/false,Waiting for deployment/#40A8E4/false"
	if got := statusNames(t, env); got != want {
		t.Fatalf("statuses:\n%s\nwant\n%s", got, want)
	}
	if got := fieldNames(t, env); got != "Testado em staging/checkbox,Executor/text" {
		t.Fatalf("fields: %s", got)
	}
	// Again: nothing to do, extras kept as unmanaged.
	out, errOut, code = runIn(t, env, applyTOML, "project", "apply", "-f", "-")
	r = applyResult(t, out)
	plan := r["plan"].(map[string]any)
	if code != 0 || r["complete"] != true || len(r["applied"].([]any)) != 0 || len(plan["actions"].([]any)) != 0 || len(plan["unmanaged"].([]any)) != 7 {
		t.Fatalf("second apply: %d %s %s", code, errOut, out)
	}
	if got := statusNames(t, env); got != want {
		t.Fatalf("second apply changed statuses: %s", got)
	}
}

// proxy forwards to the local Taiga; before decides, per request, whether to answer instead.
func proxy(t *testing.T, before func(w http.ResponseWriter, r *http.Request, body []byte) bool) string {
	t.Helper()
	base := testtaiga.URL()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if before(w, r, body) {
			return
		}
		req, _ := http.NewRequest(r.Method, base+r.URL.RequestURI(), bytes.NewReader(body))
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("forward: %v", err)
			w.WriteHeader(502)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for k, vs := range resp.Header {
			w.Header()[k] = vs
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestIntegrationProjectApplyStopsAndResumes(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-apply-partial-"+fmt.Sprint(time.Now().UnixNano()))
	toml := "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\n[[story_status]]\nname = \"B\"\ncolor = \"#000000\"\n[[story_status]]\nname = \"C\"\ncolor = \"#000000\"\n"
	posts := 0
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.Method != "POST" || r.URL.Path != "/api/v1/userstory-statuses" {
			return false
		}
		posts++
		if posts == 2 { // the second creation fails before reaching Taiga
			w.WriteHeader(502)
			return true
		}
		return false
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	out, errOut, code := runIn(t, proxied, toml, "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 7 || posts != 2 || r["complete"] != false || len(r["applied"].([]any)) != 1 || len(r["remaining"].([]any)) != 2 || !strings.Contains(errOut, "server_error") {
		t.Fatalf("partial: exit %d, %d POST: %s %s", code, posts, errOut, out)
	}
	// A new run re-plans: A is not created twice.
	out, errOut, code = runIn(t, env, toml, "project", "apply", "-f", "-")
	r = applyResult(t, out)
	if code != 0 || r["complete"] != true || len(r["applied"].([]any)) != 2 {
		t.Fatalf("resume: %d %s %s", code, errOut, out)
	}
	if got := statusNames(t, env); strings.Count(got, "A/") != 1 || !strings.HasSuffix(got, "A/#000000/false,B/#000000/false,C/#000000/false") {
		t.Fatalf("statuses: %s", got)
	}
}

func TestIntegrationProjectApplyConcurrentStatus(t *testing.T) {
	env, _, pid := freshProject(t, "cli-test-apply-race-"+fmt.Sprint(time.Now().UnixNano()))
	toml := "[[story_status]]\nname = \"Raced\"\ncolor = \"#000000\"\n"
	posts := 0
	url := proxy(t, func(_ http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.Method == "POST" && r.URL.Path == "/api/v1/userstory-statuses" {
			posts++
			// Someone creates the same status with another color between our read and our POST.
			storyJSON(t, env, "", "api", "POST", "userstory-statuses", "-F", "project="+pid, "-f", "name=Raced", "-f", "color=#FFFFFF")
		}
		return false
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	_, errOut, code := runIn(t, proxied, toml, "project", "apply", "-f", "-")
	if code != 4 || posts != 1 || !strings.Contains(errOut, "project_changed") {
		t.Fatalf("race: exit %d, %d POST: %s", code, posts, errOut)
	}
	if got := statusNames(t, env); strings.Count(got, "Raced/") != 1 || !strings.Contains(got, "Raced/#FFFFFF/false") {
		t.Fatalf("the other writer's status was not kept: %s", got)
	}
}

// The review case: another client creates a field differing only by case while apply creates a
// status, after the preflight. Apply must stop before creating its field, not end complete.
func TestIntegrationProjectApplyConcurrentFieldCase(t *testing.T) {
	env, _, pid := freshProject(t, "cli-test-apply-field-race-"+fmt.Sprint(time.Now().UnixNano()))
	toml := "[[story_status]]\nname = \"ReviewTrigger\"\ncolor = \"#000000\"\n[[story_field]]\nname = \"ReviewRace\"\ntype = \"text\"\n"
	raced := false
	url := proxy(t, func(_ http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.Method == "POST" && r.URL.Path == "/api/v1/userstory-statuses" && !raced {
			raced = true
			storyJSON(t, env, "", "api", "POST", "userstory-custom-attributes", "-F", "project="+pid, "-f", "name=reviewrace", "-f", "type=text")
		}
		return false
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	out, errOut, code := runIn(t, proxied, toml, "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 4 || !raced || r["complete"] != false || !strings.Contains(errOut, "project_changed") || len(r["applied"].([]any)) != 1 {
		t.Fatalf("exit %d: %s %s", code, errOut, out)
	}
	if got := fieldNames(t, env); got != "reviewrace/text" {
		t.Fatalf("fields: %s", got)
	}
}

func statusID(t *testing.T, env map[string]string, name string) string {
	t.Helper()
	out, errOut, code := runIn(t, env, "", "status", "list")
	if code != 0 {
		t.Fatalf("status list: %d %s", code, errOut)
	}
	var items []map[string]any
	_ = json.Unmarshal([]byte(out), &items)
	for _, it := range items {
		if it["name"] == name {
			return fmt.Sprint(it["id"])
		}
	}
	t.Fatalf("no status %q", name)
	return ""
}

func statusOrder(t *testing.T, env map[string]string) string {
	t.Helper()
	names := []string{}
	for _, s := range strings.Split(statusNames(t, env), ",") {
		names = append(names, strings.SplitN(s, "/", 2)[0])
	}
	return strings.Join(names, ",")
}

// The Basis flow example (docs/examples) applies end to end: statuses created and moved behind
// their anchors with one checked bulk write, the six fields created, a second run writes nothing.
func TestIntegrationProjectApplyBasisExample(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-apply-example-"+fmt.Sprint(time.Now().UnixNano()))
	example, err := os.ReadFile("../../docs/examples/taiga-project.toml")
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runIn(t, env, string(example), "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 0 || r["complete"] != true || len(r["applied"].([]any)) != 9 {
		t.Fatalf("apply: %d %s %s", code, errOut, out)
	}
	want := "New,Ready,In progress,In revision,Ready for test,Waiting for deployment,Done,Archived"
	if got := statusOrder(t, env); got != want {
		t.Fatalf("order:\n%s\nwant\n%s", got, want)
	}
	if got := fieldNames(t, env); got != "Início da implementação/date,Executor/text,Worktree/text,Testado em staging/checkbox,Testado por/text,Data do teste/date" {
		t.Fatalf("fields: %s", got)
	}
	out, errOut, code = runIn(t, env, string(example), "project", "apply", "-f", "-")
	r = applyResult(t, out)
	if code != 0 || r["complete"] != true || len(r["applied"].([]any)) != 0 {
		t.Fatalf("second apply: %d %s %s", code, errOut, out)
	}
	if got := statusOrder(t, env); got != want {
		t.Fatalf("second apply moved statuses: %s", got)
	}
}

const reorderTOML = "[[story_status]]\nname = \"In revision\"\ncolor = \"#5178D3\"\nafter = \"In progress\"\n"

// Someone moves a status after the plan: the re-read right before the bulk write refuses it.
func TestIntegrationProjectReorderRefusesAMovedOrder(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-reorder-before-"+fmt.Sprint(time.Now().UnixNano()))
	done := statusID(t, env, "Done")
	bulks := 0
	url := proxy(t, func(_ http.ResponseWriter, r *http.Request, _ []byte) bool {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/userstory-statuses":
			direct(t, env["TAIGA_TOKEN"], "PATCH", "/api/v1/userstory-statuses/"+done, map[string]any{"order": 0}, 200)
		case strings.HasSuffix(r.URL.Path, "/bulk_update_order"):
			bulks++
		}
		return false
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	out, errOut, code := runIn(t, proxied, reorderTOML, "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 4 || bulks != 0 || !strings.Contains(errOut, "project_changed") || len(r["applied"].([]any)) != 1 || r["complete"] != false {
		t.Fatalf("exit %d, %d bulk: %s %s", code, bulks, errOut, out)
	}
	if got := statusOrder(t, env); got != "Done,New,Ready,In progress,Ready for test,Archived,In revision" {
		t.Fatalf("the other writer's order was not kept: %s", got)
	}
}

// Someone moves a status right after our bulk write: applied, detected, never repeated.
func TestIntegrationProjectReorderPostcondition(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-reorder-after-"+fmt.Sprint(time.Now().UnixNano()))
	newID := statusID(t, env, "New")
	base := testtaiga.URL()
	bulks := 0
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if !strings.HasSuffix(r.URL.Path, "/bulk_update_order") {
			return false
		}
		bulks++
		req, _ := http.NewRequest(r.Method, base+r.URL.RequestURI(), bytes.NewReader(body))
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("forward: %v", err)
			w.WriteHeader(502)
			return true
		}
		_ = resp.Body.Close()
		direct(t, env["TAIGA_TOKEN"], "PATCH", "/api/v1/userstory-statuses/"+newID, map[string]any{"order": 100}, 200)
		w.WriteHeader(resp.StatusCode)
		return true
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	out, errOut, code := runIn(t, proxied, reorderTOML, "project", "apply", "-f", "-")
	r := applyResult(t, out)
	if code != 4 || bulks != 1 || !strings.Contains(errOut, "status_order_postcondition_failed") || !strings.Contains(errOut, "was applied") || r["complete"] != false {
		t.Fatalf("exit %d, %d bulk: %s %s", code, bulks, errOut, out)
	}
	if got := statusOrder(t, env); got != "Ready,In progress,In revision,Ready for test,Done,Archived,New" {
		t.Fatalf("order: %s", got)
	}
}
