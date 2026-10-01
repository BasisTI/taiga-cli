package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projectTOML = `[[story_status]]
name = "Waiting for deployment"
color = "#3498DB"

[[story_status]]
name = "Done"
color = "#A8E440"
closed = true

[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste da versão entregue"
`

// projectFake serves a project with the statuses and story fields of the Taiga 6.7 template.
type projectFake struct {
	srv         string
	permissions string
	statuses    []map[string]any
	fields      []map[string]any
}

func newProjectFake(t *testing.T) (*projectFake, *[]recorded) {
	f := &projectFake{
		permissions: `["view_project","admin_project_values"]`,
		statuses: []map[string]any{
			{"id": 5, "name": "Done", "slug": "done", "order": 5, "is_closed": true, "is_archived": false, "color": "#A8E440", "wip_limit": nil, "project": 37},
			{"id": 1, "name": "New", "slug": "new", "order": 1, "is_closed": false, "is_archived": false, "color": "#70728F", "wip_limit": nil, "project": 37},
			{"id": 3, "name": "In progress", "slug": "in-progress", "order": 3, "is_closed": false, "is_archived": false, "color": "#E47C40", "wip_limit": nil, "project": 37},
		},
		fields: []map[string]any{{"id": 9, "name": "Executor", "type": "text", "description": "who", "order": 1, "project": 37}},
	}
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		switch {
		case r.Method == "GET" && path == "projects/by_slug":
			_, _ = fmt.Fprint(w, `{"id":37,"slug":"infra-2025"}`)
		case r.Method == "GET" && path == "projects/37":
			_, _ = fmt.Fprintf(w, `{"id":37,"slug":"infra-2025","my_permissions":%s}`, f.permissions)
		case r.Method == "GET" && path == "userstory-statuses":
			_ = json.NewEncoder(w).Encode(f.statuses)
		case r.Method == "GET" && path == "task-statuses":
			_, _ = fmt.Fprint(w, `[{"id":2,"name":"Closed","order":2,"is_closed":true,"color":"#000000","project":37},{"id":1,"name":"New","order":1,"is_closed":false,"color":"#FFFFFF","project":37}]`)
		case r.Method == "GET" && path == "userstory-custom-attributes":
			_ = json.NewEncoder(w).Encode(f.fields)
		default:
			w.WriteHeader(404)
		}
	})
	f.srv = srv.URL
	return f, calls
}

func (f *projectFake) env() map[string]string {
	return map[string]string{"TAIGA_URL": f.srv, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
}

func TestProjectPlanGolden(t *testing.T) {
	f, calls := newProjectFake(t)
	f.permissions = `["view_project"]` // plan only reads
	out, stderr, code := runIn(t, f.env(), projectTOML, "project", "plan", "-f", "-")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "project_plan.json", out)
	text, _, _ := runIn(t, f.env(), projectTOML, "--output", "text", "project", "plan", "-f", "-")
	golden(t, "project_plan.txt", text)
	if w := writes(calls); len(w) != 0 {
		t.Fatalf("plan wrote: %v", w)
	}
}

func TestProjectApplyDryRunGolden(t *testing.T) {
	f, calls := newProjectFake(t)
	file := filepath.Join(t.TempDir(), "taiga-project.toml")
	if err := os.WriteFile(file, []byte(projectTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runIn(t, f.env(), "", "project", "apply", "-f", file, "--dry-run")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "project_apply_dry_run.json", out)
	if w := writes(calls); len(w) != 0 {
		t.Fatalf("dry-run wrote: %v", w)
	}
}

func TestProjectApplyRefusalsWriteNothing(t *testing.T) {
	for name, c := range map[string]struct {
		perms, toml string
		exit        int
		code        string
	}{
		"no admin":         {`["view_project","modify_us"]`, projectTOML, 6, "forbidden"},
		"no permissions":   {`null`, projectTOML, 6, "forbidden"},
		"drift":            {"", "[[story_status]]\nname = \"Done\"\ncolor = \"#000000\"\nclosed = true\n", 2, "definition_drift"},
		"reorder":          {"", "[[story_status]]\nname = \"In revision\"\ncolor = \"#8E44AD\"\nafter = \"In progress\"\n", 2, "unsupported_operation"},
		"cycle":            {"", "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nafter = \"B\"\n[[story_status]]\nname = \"B\"\ncolor = \"#000000\"\nafter = \"A\"\n", 2, "cycle"},
		"unknown key":      {"", "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nwip = 3\n", 2, "invalid project TOML"},
		"unknown after":    {"", "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nafter = \"Nope\"\n", 2, "unknown status"},
		"field drift type": {"", "[[story_field]]\nname = \"Executor\"\ntype = \"date\"\ndescription = \"who\"\n", 2, "definition_drift"},
	} {
		for _, dry := range []bool{false, true} {
			f, calls := newProjectFake(t)
			if c.perms != "" {
				f.permissions = c.perms
			}
			args := []string{"project", "apply", "-f", "-"}
			if dry {
				args = append(args, "--dry-run")
			}
			_, stderr, code := runIn(t, f.env(), c.toml, args...)
			if code != c.exit || !strings.Contains(stderr, c.code) || len(writes(calls)) != 0 {
				t.Errorf("%s dry=%v: exit %d %s writes %v", name, dry, code, stderr, writes(calls))
			}
		}
	}
}

func TestProjectCommandUsage(t *testing.T) {
	f, calls := newProjectFake(t)
	for _, args := range [][]string{
		{"project", "plan"},
		{"project", "apply"},
		{"project", "plan", "-f", filepath.Join(t.TempDir(), "missing.toml")},
		{"project", "plan", "-f", "-", "--dry-run"},
		{"project", "apply", "-f", "-", "--force-version"},
	} {
		if _, stderr, code := runIn(t, f.env(), "", args...); code != 2 {
			t.Errorf("%v: %d %s", args, code, stderr)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("usage errors reached the network: %v", *calls)
	}
}

func TestStatusListInBoardOrder(t *testing.T) {
	f, _ := newProjectFake(t)
	out, stderr, code := runIn(t, f.env(), "", "status", "list")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 3 || items[0]["name"] != "New" || items[2]["name"] != "Done" || items[0]["slug"] != "new" {
		t.Fatalf("%v %s", err, out)
	}
	text, _, _ := runIn(t, f.env(), "", "--output", "text", "status", "list", "--kind", "task")
	if !strings.HasPrefix(text, "id:         1\nname:       New\norder:      1\nis_closed:  false\ncolor:      #FFFFFF\n\nid:") {
		t.Fatalf("text:\n%s", text)
	}
	if _, stderr, code := runIn(t, f.env(), "", "status", "list", "--kind", "epic"); code != 2 {
		t.Fatalf("kind: %d %s", code, stderr)
	}
}
