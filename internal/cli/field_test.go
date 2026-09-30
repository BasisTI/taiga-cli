package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

var defPaths = map[string]bool{"userstory-custom-attributes": true, "task-custom-attributes": true}

// handleFields serves definitions, values and tasks the way the local Taiga 6.7 does
// (docs/api-notes.md): a values PATCH replaces the whole dictionary and accepts any version up
// to the current one. It reports whether it handled the request.
func (f *storyFake) handleFields(w http.ResponseWriter, r *http.Request, path string) bool {
	q := r.URL.Query()
	head, tail, _ := strings.Cut(path, "/")
	switch {
	case defPaths[path] && r.Method == "GET":
		if q.Get("project") != "37" {
			f.t.Errorf("GET %s without project=37: %s", path, r.URL.RawQuery)
		}
		f.list(w, r, f.defs[path])
	case defPaths[head] && r.Method == "GET":
		for _, d := range f.defs[head] {
			if fmt.Sprint(d["id"]) == tail {
				f.write(w, d)
				return true
			}
		}
		w.WriteHeader(404)
	case defPaths[path] && r.Method == "POST":
		if f.onDefPost != nil {
			f.onDefPost()
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, d := range f.defs[path] {
			if d["name"] == body["name"] {
				w.WriteHeader(400)
				_, _ = fmt.Fprint(w, `{"name":["Already exists one with the same name."],"project":["Already exists one with the same name."]}`)
				return true
			}
		}
		f.nextID++
		d := map[string]any{"id": f.nextID, "name": body["name"], "type": body["type"], "description": "", "order": 1, "project": 37, "extra": nil}
		if desc, ok := body["description"]; ok {
			d["description"] = desc
		}
		if f.defs == nil {
			f.defs = map[string][]map[string]any{}
		}
		f.defs[path] = append(f.defs[path], d)
		w.WriteHeader(201)
		if f.badWrite {
			_, _ = fmt.Fprint(w, `<html>proxy</html>`)
			return true
		}
		f.write(w, d)
	case strings.HasPrefix(path, "userstories/custom-attributes-values/") || strings.HasPrefix(path, "tasks/custom-attributes-values/"):
		f.handleValues(w, r, path)
	case head == "tasks" && r.Method == "GET":
		id, _ := strconv.ParseInt(tail, 10, 64)
		if t, ok := f.tasks[id]; ok {
			f.write(w, t)
			return true
		}
		w.WriteHeader(404)
	default:
		return false
	}
	return true
}

func (f *storyFake) handleValues(w http.ResponseWriter, r *http.Request, path string) {
	v, ok := f.values[path]
	if !ok {
		w.WriteHeader(404)
		_, _ = fmt.Fprint(w, `{"_error_message":"No matches the given query."}`)
		return
	}
	if f.onValues != nil {
		f.onValues(r.Method, v)
	}
	if r.Method == "GET" {
		f.write(w, v)
		return
	}
	if r.Method != "PATCH" {
		f.t.Errorf("unexpected %s %s", r.Method, path)
		w.WriteHeader(405)
		return
	}
	var body map[string]any
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	_ = d.Decode(&body)
	version, err := strconv.Atoi(fmt.Sprint(body["version"]))
	if err != nil || version < 0 || version > v["version"].(int) {
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, `{"version":"The version parameter is not valid"}`)
		return
	}
	values, _ := body["attributes_values"].(map[string]any)
	if len(values) == 0 {
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, `{"attributes_values":["This field cannot be blank."]}`)
		return
	}
	v["attributes_values"] = values
	v["version"] = v["version"].(int) + 1
	if f.badValuesWrite {
		_, _ = fmt.Fprint(w, `<html>proxy</html>`)
		return
	}
	f.write(w, v)
}

func fieldFake(t *testing.T) (*storyFake, *[]recorded) {
	f, calls := newStoryFake(t)
	f.defs = map[string][]map[string]any{
		"userstory-custom-attributes": {
			{"id": 27, "name": "Testado em staging", "type": "checkbox", "description": "", "order": 1, "project": 37},
			{"id": 28, "name": "Data de entrega", "type": "date", "description": "prazo", "order": 2, "project": 37},
			{"id": 29, "name": "Notas", "type": "text", "description": "", "order": 3, "project": 37},
		},
		"task-custom-attributes": {
			{"id": 27, "name": "Horas", "type": "text", "description": "", "order": 1, "project": 37},
		},
	}
	return f, calls
}

func TestFieldListUsesTheKindRoute(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "field", "list", "--kind", "story")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 3 || items[1]["name"] != "Data de entrega" || items[1]["description"] != "prazo" {
		t.Fatalf("%v %s", err, out)
	}
	out, _, code = runIn(t, f.env(), "", "field", "list", "--kind", "task", "--output", "text")
	if code != 0 || !strings.Contains(out, "name") || !strings.Contains(out, "Horas") || !strings.Contains(out, "text") {
		t.Fatalf("%d %s", code, out)
	}
	for _, c := range *calls {
		if strings.HasSuffix(c.path, "-custom-attributes") && !strings.Contains("&"+c.query+"&", "&project=37&") {
			t.Fatalf("catalog without project: %+v", c)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatal("list wrote")
	}
}

func TestFieldListEmptyIsAnArray(t *testing.T) {
	f, _ := fieldFake(t)
	f.defs["task-custom-attributes"] = nil
	out, _, code := runIn(t, f.env(), "", "field", "list", "--kind", "task")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("%d %q", code, out)
	}
}

func TestFieldCreatePostsOnceAndConfirms(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "task", "--name", "Revisado", "--type", "checkbox", "--description", "com \"aspas\"")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil || created["name"] != "Revisado" || created["description"] != "com \"aspas\"" {
		t.Fatalf("%v %s", err, out)
	}
	w := writes(calls)
	if len(w) != 1 || w[0].path != "/api/v1/task-custom-attributes" || w[0].body["project"] != float64(37) || w[0].body["type"] != "checkbox" {
		t.Fatalf("%+v", w)
	}
	if _, ok := w[0].body["version"]; ok {
		t.Fatal("create sent a version")
	}
	last := (*calls)[len(*calls)-1]
	if last.method != "GET" || last.path != fmt.Sprintf("/api/v1/task-custom-attributes/%v", created["id"]) {
		t.Fatalf("no confirmation read: %+v", last)
	}
}

func TestFieldCreateWithoutDescriptionOmitsIt(t *testing.T) {
	f, calls := fieldFake(t)
	if _, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Nova", "--type", "text"); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if _, ok := writes(calls)[0].body["description"]; ok {
		t.Fatal("description sent without the flag")
	}
}

func TestFieldCreateIsIdempotent(t *testing.T) {
	for _, args := range [][]string{
		{"--name", "Data de entrega", "--type", "date"},
		{"--name", "Data de entrega", "--type", "date", "--description", "prazo"},
		{"--name", "Testado em staging", "--type", "checkbox", "--description", ""},
	} {
		f, calls := fieldFake(t)
		out, stderr, code := runIn(t, f.env(), "", append([]string{"field", "create", "--kind", "story"}, args...)...)
		if code != 0 || !strings.Contains(out, `"id": 2`) {
			t.Fatalf("%v: %d %s %s", args, code, stderr, out)
		}
		if len(writes(calls)) != 0 {
			t.Fatalf("%v: wrote %+v", args, writes(calls))
		}
	}
}

func TestFieldCreateRefusesADifferentDefinition(t *testing.T) {
	for _, args := range [][]string{
		{"--name", "Data de entrega", "--type", "text"},
		{"--name", "Data de entrega", "--type", "date", "--description", "outro"},
		{"--name", "Data de entrega", "--type", "date", "--description", ""},
	} {
		f, calls := fieldFake(t)
		_, stderr, code := runIn(t, f.env(), "", append([]string{"field", "create", "--kind", "story"}, args...)...)
		if code != 2 || !strings.Contains(stderr, "field_definition_conflict") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		if len(writes(calls)) != 0 {
			t.Fatalf("%v: wrote", args)
		}
	}
}

func TestFieldCreateNameIsCaseSensitive(t *testing.T) {
	f, calls := fieldFake(t)
	if _, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "notas", "--type", "date"); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if len(writes(calls)) != 1 {
		t.Fatal("expected a POST for another case")
	}
}

func TestFieldCreateRefusesDuplicateNames(t *testing.T) {
	f, calls := fieldFake(t)
	f.defs["userstory-custom-attributes"] = append(f.defs["userstory-custom-attributes"],
		map[string]any{"id": 40, "name": "Notas", "type": "text", "description": "", "project": 37})
	_, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Notas", "--type", "text")
	if code != 2 || !strings.Contains(stderr, "ambiguous_name") || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestFieldCreateDryRunDoesNotPost(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Nova", "--type", "date", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("%d %s %+v", code, stderr, writes(calls))
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan["dry_run"] != true || plan["method"] != "POST" || plan["path"] != "userstory-custom-attributes" {
		t.Fatalf("%v %s", err, out)
	}
	if body := plan["body"].(map[string]any); body["name"] != "Nova" || body["project"] != float64(37) {
		t.Fatalf("%v", body)
	}
}

// Another run creates the definition between our catalog read and our POST: Taiga refuses the
// duplicate, and the definition read again is accepted only when compatible.
func TestFieldCreateRaceRereadsAfterRefusal(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		code int
	}{{"text", 0}, {"date", 2}} {
		f, calls := fieldFake(t)
		f.onDefPost = func() {
			f.defs["userstory-custom-attributes"] = append(f.defs["userstory-custom-attributes"],
				map[string]any{"id": 50, "name": "Corrida", "type": "text", "description": "", "project": 37})
		}
		out, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Corrida", "--type", tc.typ)
		if code != tc.code {
			t.Fatalf("%s: %d %s %s", tc.typ, code, stderr, out)
		}
		if code == 0 && !strings.Contains(out, `"id": 50`) {
			t.Fatalf("%s", out)
		}
		if len(writes(calls)) != 1 {
			t.Fatalf("POST repeated: %+v", writes(calls))
		}
	}
}

func TestFieldCreateUnreadableAnswerIsWriteApplied(t *testing.T) {
	f, calls := fieldFake(t)
	f.badWrite = true
	_, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Nova", "--type", "text")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestFieldRejectsBadInputBeforeNetwork(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "p"}
	for _, args := range [][]string{
		{"field", "list"},
		{"field", "list", "--kind", "issue"},
		{"field", "create", "--name", "x", "--type", "text"},
		{"field", "create", "--kind", "story", "--type", "text"},
		{"field", "create", "--kind", "story", "--name", " ", "--type", "text"},
		{"field", "create", "--kind", "story", "--name", "x"},
		{"field", "create", "--kind", "story", "--name", "x", "--type", "number"},
		{"field", "list", "--kind", "story", "extra"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}
