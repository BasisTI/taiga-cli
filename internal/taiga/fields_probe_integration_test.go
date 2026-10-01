//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// probeProjectFields has svc as a member without admin rights, to probe the permissions.
const probeProjectFields = "cli-test-probe-fields"

func svcClient(t *testing.T) *Client {
	t.Helper()
	base := testtaiga.URL()
	token, _ := testtaiga.Login(t, testtaiga.ServiceUser, testtaiga.ServicePassword)
	return New(base, StaticToken{Type: "Bearer", Value: token}, WithRetryWait(0))
}

func fieldsProject(t *testing.T) (*Client, int64) {
	t.Helper()
	c := probeClient(t)
	project := ensureProject(t, c, probeProjectFields)
	svc := userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svc, testtaiga.ServiceEmail)
	return c, project
}

func probeErr(t *testing.T, c *Client, method, path string, body any) (int, map[string]json.RawMessage) {
	t.Helper()
	_, err := c.Do(context.Background(), Request{Method: method, Path: path, Body: body})
	if err == nil {
		t.Fatalf("%s %s: accepted", method, path)
	}
	ae, _ := err.(*APIError)
	if ae == nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(ae.Body, &m)
	return ae.Status, m
}

func TestProbeFieldDefinitions(t *testing.T) {
	c, project := fieldsProject(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	for _, path := range []string{"userstory-custom-attributes", "task-custom-attributes"} {
		name := "probe " + suffix
		for _, typ := range []string{"text", "date", "checkbox"} {
			d := probeDo(t, c, "POST", path, nil, map[string]any{"project": project, "name": name + " " + typ, "type": typ})
			if string(d["type"]) != fmt.Sprintf("%q", typ) || string(d["description"]) != `""` {
				t.Fatalf("%s %s: %v", path, typ, d)
			}
			if _, ok := d["version"]; ok {
				t.Fatalf("%s: definitions have a version: %v", path, d)
			}
			got := probeDo(t, c, "GET", fmt.Sprintf("%s/%d", path, d.int("id")), nil, nil)
			if string(got["name"]) != string(d["name"]) || got.int("project") != project {
				t.Fatalf("re-read: %v", got)
			}
		}
		listed := probeList(t, c, path, url.Values{"project": {fmt.Sprint(project)}})
		found := 0
		for _, d := range listed {
			if strings.HasPrefix(string(d["name"]), fmt.Sprintf("%q", name)[:len(name)+1]) {
				found++
			}
		}
		if found != 3 {
			t.Fatalf("%s: listed %d of 3", path, found)
		}

		// Same name twice: refused, with a "name" key. Another case is another name.
		status, body := probeErr(t, c, "POST", path, map[string]any{"project": project, "name": name + " text", "type": "text"})
		if _, ok := body["name"]; status != 400 || !ok {
			t.Fatalf("%s duplicate: %d %v", path, status, body)
		}
		probeDo(t, c, "POST", path, nil, map[string]any{"project": project, "name": strings.ToUpper(name + " text"), "type": "text"})
		if status, body = probeErr(t, c, "POST", path, map[string]any{"project": project, "name": name + " bogus", "type": "bogus"}); status != 400 || body["type"] == nil {
			t.Fatalf("bogus type: %d %v", status, body)
		}
		if status, body = probeErr(t, c, "POST", path, map[string]any{"project": project, "name": strings.Repeat("x", 65), "type": "text"}); status != 400 || body["name"] == nil {
			t.Fatalf("65 characters: %d %v", status, body)
		}

		// A member without admin rights reads the definitions but cannot create one.
		svc := svcClient(t)
		if len(probeList(t, svc, path, url.Values{"project": {fmt.Sprint(project)}})) != len(listed)+1 {
			t.Fatalf("%s: svc sees another list", path)
		}
		if status, _ := probeErr(t, svc, "POST", path, map[string]any{"project": project, "name": "svc " + suffix, "type": "text"}); status != 403 {
			t.Fatalf("svc create: %d", status)
		}
	}
}

func valuesOf(t *testing.T, o probeObject) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(o["attributes_values"], &m); err != nil {
		t.Fatalf("attributes_values %s: %v", o["attributes_values"], err)
	}
	return m
}

func TestProbeFieldValues(t *testing.T) {
	c, project := fieldsProject(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	def := func(path, typ string) string {
		return fmt.Sprint(probeDo(t, c, "POST", path, nil, map[string]any{"project": project, "name": typ + " " + suffix, "type": typ}).int("id"))
	}
	text, date, check := def("userstory-custom-attributes", "text"), def("userstory-custom-attributes", "date"), def("userstory-custom-attributes", "checkbox")
	story := createStory(t, c, project, "values "+suffix, nil)
	path := fmt.Sprintf("userstories/custom-attributes-values/%d", story.int("id"))

	v := probeDo(t, c, "GET", path, nil, nil)
	if v.int("version") != 1 || v.int("user_story") != story.int("id") || len(valuesOf(t, v)) != 0 {
		t.Fatalf("new story values: %v", v)
	}
	set := map[string]any{text: "a=b \"ç\"\n", date: "2026-09-30", check: false}
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 1, "attributes_values": set})
	if v.int("version") != 2 {
		t.Fatalf("patch: %v", v)
	}
	got := probeDo(t, c, "GET", path, nil, nil)
	if !reflect.DeepEqual(valuesOf(t, got), set) {
		t.Fatalf("persisted: %v", got)
	}
	if s := probeDo(t, c, "GET", fmt.Sprintf("userstories/%d", story.int("id")), nil, nil); s.int("version") != story.int("version") {
		t.Fatalf("the story version moved: %d → %d", story.int("version"), s.int("version"))
	}

	// The PATCH replaces the whole dictionary.
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 2, "attributes_values": map[string]any{text: "only"}})
	if m := valuesOf(t, v); len(m) != 1 || m[text] != "only" {
		t.Fatalf("replace: %v", m)
	}
	// An old version is accepted even though the dictionary changed since: no conflict.
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 1, "attributes_values": map[string]any{date: "2026-01-01"}})
	if m := valuesOf(t, v); v.int("version") != 4 || len(m) != 1 || m[date] != "2026-01-01" {
		t.Fatalf("stale version: %v", v)
	}
	// A version above the current one, or none, is refused.
	for _, body := range []map[string]any{{"version": 99, "attributes_values": set}, {"attributes_values": set}} {
		if status, m := probeErr(t, c, "PATCH", path, body); status != 400 || m["version"] == nil {
			t.Fatalf("%v: %d %v", body, status, m)
		}
	}
	// null for date and checkbox, "" for date and any text for checkbox: the server keeps them.
	loose := map[string]any{date: nil, check: nil, text: ""}
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 4, "attributes_values": loose})
	if !reflect.DeepEqual(valuesOf(t, v), loose) {
		t.Fatalf("null: %v", v)
	}
	loose = map[string]any{date: "30/09/2026", check: "yes"}
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 5, "attributes_values": loose})
	if !reflect.DeepEqual(valuesOf(t, v), loose) {
		t.Fatalf("unvalidated: %v", v)
	}
	// An empty dictionary and an id outside the project's definitions are refused.
	if status, m := probeErr(t, c, "PATCH", path, map[string]any{"version": 6, "attributes_values": map[string]any{}}); status != 400 || m["attributes_values"] == nil {
		t.Fatalf("empty: %d %v", status, m)
	}
	if status, m := probeErr(t, c, "PATCH", path, map[string]any{"version": 6, "attributes_values": map[string]any{"999999": "x"}}); status != 400 || m["attributes_values"] == nil {
		t.Fatalf("unknown id: %d %v", status, m)
	}

	// A member without admin rights writes values.
	v = probeDo(t, svcClient(t), "PATCH", path, nil, map[string]any{"version": 6, "attributes_values": map[string]any{text: "svc"}})
	if v.int("version") != 7 {
		t.Fatalf("svc: %v", v)
	}
}

func TestProbeTaskFieldValues(t *testing.T) {
	c, project := fieldsProject(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	def := fmt.Sprint(probeDo(t, c, "POST", "task-custom-attributes", nil, map[string]any{"project": project, "name": "task " + suffix, "type": "text"}).int("id"))
	task := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "subject": "task " + suffix})
	byRef := probeDo(t, c, "GET", "tasks/by_ref", url.Values{"project": {fmt.Sprint(project)}, "ref": {fmt.Sprint(task.int("ref"))}}, nil)
	if byRef.int("id") != task.int("id") {
		t.Fatalf("tasks/by_ref: %v", byRef)
	}
	path := fmt.Sprintf("tasks/custom-attributes-values/%d", task.int("id"))
	v := probeDo(t, c, "GET", path, nil, nil)
	if v.int("version") != 1 || v.int("task") != task.int("id") {
		t.Fatalf("task values: %v", v)
	}
	v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 1, "attributes_values": map[string]any{def: "8h"}})
	if got := probeDo(t, c, "GET", path, nil, nil); v.int("version") != 2 || valuesOf(t, got)[def] != "8h" {
		t.Fatalf("task persisted: %v", got)
	}
	if again := probeDo(t, c, "GET", fmt.Sprintf("tasks/%d", task.int("id")), nil, nil); again.int("version") != task.int("version") {
		t.Fatalf("the task version moved: %v", again["version"])
	}
}

// US #260: unset is a null value kept under its key. The read returns the key with null, a
// null for a key that was absent adds it (the server stores it, so it is not a no-op there),
// and a dictionary whose only key is null is accepted: clearing the last field keeps the key.
func TestProbeFieldValuesUnset(t *testing.T) {
	c, project := fieldsProject(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	def := func(path, typ string) string {
		return fmt.Sprint(probeDo(t, c, "POST", path, nil, map[string]any{"project": project, "name": "unset " + typ + " " + suffix, "type": typ}).int("id"))
	}
	for _, kind := range []struct{ defs, values, create string }{
		{"userstory-custom-attributes", "userstories/custom-attributes-values/%d", "story"},
		{"task-custom-attributes", "tasks/custom-attributes-values/%d", "task"},
	} {
		date, check := def(kind.defs, "date"), def(kind.defs, "checkbox")
		var owner probeObject
		if kind.create == "story" {
			owner = createStory(t, c, project, "unset "+suffix, nil)
		} else {
			owner = probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "subject": "unset " + suffix})
		}
		path := fmt.Sprintf(kind.values, owner.int("id"))

		probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 1, "attributes_values": map[string]any{date: "2026-09-30", check: true}})
		v := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 2, "attributes_values": map[string]any{date: nil, check: true}})
		got := probeDo(t, c, "GET", path, nil, nil)
		if m := valuesOf(t, got); v.int("version") != 3 || len(m) != 2 || m[date] != nil || m[check] != true {
			t.Fatalf("%s read null (the key must stay): %s", kind.create, got["attributes_values"])
		}
		// The last field set to null: accepted, with the key kept.
		v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 3, "attributes_values": map[string]any{check: nil}})
		if m := valuesOf(t, probeDo(t, c, "GET", path, nil, nil)); v.int("version") != 4 || len(m) != 1 || m[check] != nil {
			t.Fatalf("%s last field: %v", kind.create, m)
		}
		// null for an absent key: stored as a new key, and the version moves.
		v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 4, "attributes_values": map[string]any{check: nil, date: nil}})
		if m := valuesOf(t, probeDo(t, c, "GET", path, nil, nil)); v.int("version") != 5 || len(m) != 2 {
			t.Fatalf("%s absent key: %v", kind.create, m)
		}
		// The same dictionary again: the server still bumps the version.
		v = probeDo(t, c, "PATCH", path, nil, map[string]any{"version": 5, "attributes_values": map[string]any{check: nil, date: nil}})
		if v.int("version") != 6 {
			t.Fatalf("%s same dictionary: %v", kind.create, v)
		}
	}
}
