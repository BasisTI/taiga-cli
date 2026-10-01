//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestIntegrationFieldDefinitionsAreIdempotent(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	for _, kind := range []string{"story", "task"} {
		name := "Revisado " + suffix
		create := []string{"field", "create", "--kind", kind, "--name", name, "--type", "checkbox", "--description", "com \"aspas\" e ção"}
		plan := storyJSON(t, env, "", append(create, "--dry-run")...)
		if plan["dry_run"] != true || countNamed(t, env, kind, name) != 0 {
			t.Fatalf("%s dry-run: %v", kind, plan)
		}
		first := storyJSON(t, env, "", create...)
		again := storyJSON(t, env, "", create...)
		if first["id"] != again["id"] || first["type"] != "checkbox" || first["description"] != "com \"aspas\" e ção" {
			t.Fatalf("%s: %v %v", kind, first, again)
		}
		if n := countNamed(t, env, kind, name); n != 1 {
			t.Fatalf("%s: %d definitions", kind, n)
		}
		if _, errOut, code := runIn(t, env, "", "field", "create", "--kind", kind, "--name", name, "--type", "text"); code != 2 || !strings.Contains(errOut, "field_definition_conflict") {
			t.Fatalf("%s incompatible: %d %s", kind, code, errOut)
		}
	}
}

func countNamed(t *testing.T, env map[string]string, kind, name string) int {
	t.Helper()
	out, errOut, code := runIn(t, env, "", "field", "list", "--kind", kind)
	if code != 0 {
		t.Fatalf("list: %d %s", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range items {
		if it["name"] == name {
			n++
		}
	}
	return n
}

func TestIntegrationStoryFieldValues(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	names := map[string]string{"text": "Notas " + suffix, "date": "Entrega " + suffix, "checkbox": "Staging " + suffix}
	ids := map[string]string{}
	for typ, name := range names {
		ids[typ] = fmt.Sprint(storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", name, "--type", typ)["id"])
	}
	created := storyJSON(t, env, "", "story", "create", "--subject", "fields "+suffix)
	ref := fmt.Sprint(created["ref"])

	text := "a=b \"aspas\"\n\tção"
	set := []string{"story", "field", "set", ref, names["text"] + "=" + text, names["date"] + "=2026-09-30", names["checkbox"] + "=false"}
	plan := storyJSON(t, env, "", append(set, "--dry-run")...)
	if plan["dry_run"] != true || storyJSON(t, env, "", "story", "field", "list", ref)["version"] != float64(1) {
		t.Fatalf("dry-run wrote: %v", plan)
	}
	written := storyJSON(t, env, "", set...)
	want := map[string]any{ids["text"]: text, ids["date"]: "2026-09-30", ids["checkbox"]: false}
	if written["version"] != float64(2) || !reflect.DeepEqual(written["attributes_values"], want) {
		t.Fatalf("set: %v", written)
	}
	// Persisted: a new run reads it back; the story's own version did not move.
	listed := storyJSON(t, env, "", "story", "field", "list", ref)
	if !reflect.DeepEqual(listed["attributes_values"], want) {
		t.Fatalf("persisted: %v", listed)
	}
	story := storyJSON(t, env, "", "story", "get", ref)
	ca, _ := story["custom_attributes"].(map[string]any)
	if story["version"] != created["version"] || ca["version"] != float64(2) || !reflect.DeepEqual(ca["attributes_values"], want) {
		t.Fatalf("story get: %v", story)
	}

	// Merge: one field changes, the others stay; an unchanged value writes nothing.
	merged := storyJSON(t, env, "", "story", "field", "set", ref, names["checkbox"]+"=true")
	want[ids["checkbox"]] = true
	if merged["version"] != float64(3) || !reflect.DeepEqual(merged["attributes_values"], want) {
		t.Fatalf("merge: %v", merged)
	}
	if same := storyJSON(t, env, "", "story", "field", "set", ref, names["checkbox"]+"=true"); same["version"] != float64(3) {
		t.Fatalf("no-op wrote: %v", same)
	}
	if _, errOut, code := runIn(t, env, "", "story", "field", "set", ref, names["date"]+"=2026-02-30"); code != 2 || !strings.Contains(errOut, "YYYY-MM-DD") {
		t.Fatalf("bad date: %d %s", code, errOut)
	}
	if v := storyJSON(t, env, "", "story", "field", "list", ref)["version"]; v != float64(3) {
		t.Fatalf("a refused set wrote: %v", v)
	}
}

func TestIntegrationTaskFieldValues(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	name := "Horas " + suffix
	def := storyJSON(t, env, "", "field", "create", "--kind", "task", "--name", name, "--type", "text")
	project := storyJSON(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	task := storyJSON(t, env, "", "api", "POST", "tasks", "-F", fmt.Sprintf("project=%v", project["id"]), "-f", "subject=task "+suffix)

	ctx := context.Background()
	s, err := app.New(ctx, taiga.New(testtaiga.URL(), taiga.StaticToken{Type: "Bearer", Value: token}), testtaiga.ProjectSlug)
	if err != nil {
		t.Fatal(err)
	}
	id := app.ID(task["id"])
	plan, err := s.SetFieldValues(ctx, "task", id, []string{name + "=8h"}, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := plan.(app.WritePlan); !ok || p.Path != fmt.Sprintf("tasks/custom-attributes-values/%d", id) {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := s.SetFieldValues(ctx, "task", id, []string{name + "=8h"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	values, err := s.FieldValues(ctx, "task", id)
	if err != nil {
		t.Fatal(err)
	}
	stored := values["attributes_values"].(map[string]any)
	if app.ID(values["version"]) != 2 || stored[fmt.Sprint(def["id"])] != "8h" {
		t.Fatalf("task values: %v", values)
	}
	again := storyJSON(t, env, "", "api", "GET", fmt.Sprintf("tasks/%d", id))
	if again["version"] != task["version"] {
		t.Fatalf("the task version moved: %v → %v", task["version"], again["version"])
	}

	// US #260: the task values service clears a checkbox the same way.
	check := storyJSON(t, env, "", "field", "create", "--kind", "task", "--name", "Pago "+suffix, "--type", "checkbox")
	if s, err = app.New(ctx, taiga.New(testtaiga.URL(), taiga.StaticToken{Type: "Bearer", Value: token}), testtaiga.ProjectSlug); err != nil {
		t.Fatal(err) // a new service: the catalog read above is cached
	}
	if _, err := s.SetFieldValues(ctx, "task", id, []string{check["name"].(string) + "=true"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFieldValues(ctx, "task", id, nil, []string{name}, false, false); err == nil {
		t.Fatal("text unset accepted")
	}
	cleared, err := s.SetFieldValues(ctx, "task", id, nil, []string{check["name"].(string)}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	stored = cleared.(app.Object)["attributes_values"].(map[string]any)
	if v, has := stored[fmt.Sprint(check["id"])]; app.ID(cleared.(app.Object)["version"]) != 4 || !has || v != nil || stored[fmt.Sprint(def["id"])] != "8h" {
		t.Fatalf("task unset: %v", cleared)
	}
}

// US #260: --unset stores null and keeps the key, also for the last field; an absent or
// already null field writes nothing; story get and field list show the null.
func TestIntegrationStoryFieldUnset(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	names := map[string]string{"text": "Unset notas " + suffix, "date": "Unset entrega " + suffix, "checkbox": "Unset staging " + suffix}
	ids := map[string]string{}
	for typ, name := range names {
		ids[typ] = fmt.Sprint(storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", name, "--type", typ)["id"])
	}
	ref := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "unset "+suffix)["ref"])
	version := func() any { return storyJSON(t, env, "", "story", "field", "list", ref)["version"] }

	storyJSON(t, env, "", "story", "field", "set", ref, names["checkbox"]+"=true", names["date"]+"=2026-09-30")
	plan := storyJSON(t, env, "", "story", "field", "set", ref, "--unset", names["date"], "--dry-run")
	if plan["dry_run"] != true || version() != float64(2) {
		t.Fatalf("dry-run wrote: %v", plan)
	}
	cleared := storyJSON(t, env, "", "story", "field", "set", ref, "--unset", names["date"])
	want := map[string]any{ids["checkbox"]: true, ids["date"]: nil}
	if cleared["version"] != float64(3) || !reflect.DeepEqual(cleared["attributes_values"], want) {
		t.Fatalf("unset date: %v", cleared)
	}
	// The last field with a value: the dictionary keeps both keys, with null.
	cleared = storyJSON(t, env, "", "story", "field", "set", ref, "--unset", names["checkbox"])
	want[ids["checkbox"]] = nil
	if cleared["version"] != float64(4) || !reflect.DeepEqual(cleared["attributes_values"], want) {
		t.Fatalf("unset checkbox: %v", cleared)
	}
	listed := storyJSON(t, env, "", "story", "field", "list", ref)
	for _, f := range listed["fields"].([]any) {
		field := f.(map[string]any)
		value, has := field["value"]
		switch fmt.Sprint(field["id"]) {
		case ids["date"], ids["checkbox"]:
			if !has || value != nil {
				t.Fatalf("cleared field %v: %v", field["name"], field)
			}
		case ids["text"]:
			if has {
				t.Fatalf("text field without a value: %v", field)
			}
		}
	}
	ca, _ := storyJSON(t, env, "", "story", "get", ref)["custom_attributes"].(map[string]any)
	if ca["version"] != float64(4) || !reflect.DeepEqual(ca["attributes_values"], want) {
		t.Fatalf("story get: %v", ca)
	}
	text, _, code := runIn(t, env, "", "story", "field", "list", ref, "--output", "text")
	if line := lineOf(text, names["checkbox"]+" (checkbox):"); code != 0 || strings.TrimSpace(strings.TrimPrefix(line, names["checkbox"]+" (checkbox):")) != "" {
		t.Fatalf("text:\n%s", text)
	}

	// Already null, or never set: nothing is written.
	if again := storyJSON(t, env, "", "story", "field", "set", ref, "--unset", names["checkbox"]); again["version"] != float64(4) {
		t.Fatalf("null again wrote: %v", again)
	}
	other := fmt.Sprint(storyJSON(t, env, "", "story", "create", "--subject", "unset absent "+suffix)["ref"])
	if absent := storyJSON(t, env, "", "story", "field", "set", other, "--unset", names["date"]); absent["version"] != float64(1) || len(absent["attributes_values"].(map[string]any)) != 0 {
		t.Fatalf("absent key wrote: %v", absent)
	}

	// Text cannot be unset; the text "null" is text, never a cleared value.
	if _, errOut, code := runIn(t, env, "", "story", "field", "set", ref, "--unset", names["text"]); code != 2 || !strings.Contains(errOut, "only checkbox and date") {
		t.Fatalf("text unset: %d %s", code, errOut)
	}
	written := storyJSON(t, env, "", "story", "field", "set", ref, names["text"]+"=null")
	if written["version"] != float64(5) || written["attributes_values"].(map[string]any)[ids["text"]] != "null" {
		t.Fatalf("text null: %v", written)
	}
}

// The review case: someone writes key B between our read and our PATCH; our PATCH overwrites
// the dictionary without B (Taiga accepts the old version) and its answer is cut after the
// 200. The CLI must re-read and report the applied, clashing write (exit 4), not a network
// error (exit 7) that scripts would repeat.
func TestIntegrationTruncatedValuesAnswerIsDetected(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	base := testtaiga.URL()
	env := map[string]string{"TAIGA_URL": base, "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	suffix := fmt.Sprint(time.Now().UnixNano())
	a := storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", "Truncated A "+suffix, "--type", "text")
	b := storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", "Truncated B "+suffix, "--type", "text")
	story := storyJSON(t, env, "", "story", "create", "--subject", "truncated "+suffix)
	path := fmt.Sprintf("/api/v1/userstories/custom-attributes-values/%v", story["id"])

	direct := func(method string, body any) {
		t.Helper()
		payload, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s %s: %v %v", method, path, err, resp)
		}
		_ = resp.Body.Close()
	}
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == "PATCH" && r.URL.Path == path {
			patches++
			direct("PATCH", map[string]any{"version": 1, "attributes_values": map[string]any{fmt.Sprint(b["id"]): "other person"}})
		}
		req, _ := http.NewRequest(r.Method, base+r.URL.RequestURI(), bytes.NewReader(body))
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
		if r.Method == "PATCH" && r.URL.Path == path {
			_, _ = w.Write(answer[:10]) // the connection drops mid-body
			return
		}
		_, _ = w.Write(answer)
	}))
	defer srv.Close()

	proxied := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": token, "TAIGA_PROJECT": testtaiga.ProjectSlug}
	_, errOut, code := runIn(t, proxied, "", "story", "field", "set", fmt.Sprint(story["ref"]), fmt.Sprintf("%v=mine", a["name"]))
	if code != 4 || !strings.Contains(errOut, "field_values_postcondition_failed") || !strings.Contains(errOut, "the change was applied") || patches != 1 {
		t.Fatalf("exit %d, %d PATCH: %s", code, patches, errOut)
	}
	// B was lost, as the error says: the CLI detects it, it cannot prevent it.
	values := storyJSON(t, env, "", "story", "field", "list", fmt.Sprint(story["ref"]))
	if got := values["attributes_values"].(map[string]any); got[fmt.Sprint(a["id"])] != "mine" || got[fmt.Sprint(b["id"])] != nil || values["version"] != float64(3) {
		t.Fatalf("values: %v", values)
	}
}
