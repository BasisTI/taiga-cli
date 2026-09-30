//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
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
	plan, err := s.SetFieldValues(ctx, "task", id, []string{name + "=8h"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := plan.(app.WritePlan); !ok || p.Path != fmt.Sprintf("tasks/custom-attributes-values/%d", id) {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := s.SetFieldValues(ctx, "task", id, []string{name + "=8h"}, false, false); err != nil {
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
}
