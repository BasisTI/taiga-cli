package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestDiagnoseReportsSandboxAndMissingSession(t *testing.T) {
	env := map[string]string{"CODEX_SANDBOX_NETWORK_DISABLED": "1"}
	checks := Diagnose(context.Background(), DiagnoseInput{Env: func(k string) string { return env[k] }, Store: Store{Dir: t.TempDir()}, Ref: "x", Now: time.Now})
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["sandbox"].Status != "failed" || byName["session_cache"].Status != "failed" || byName["env_token"].Status != "skipped" || byName["tty"].Status != "skipped" {
		t.Fatalf("%+v", checks)
	}
}

func TestDiagnoseKeyringFailurePointsToReadme(t *testing.T) {
	probe := func(context.Context) error {
		return authErr("keyring_service_unavailable", "keyring", "none", keyringRecovery)
	}
	for _, c := range Diagnose(context.Background(), DiagnoseInput{Env: func(string) string { return "" }, Store: Store{Dir: t.TempDir()}, Ref: "x", KeyringProbe: probe, Now: time.Now}) {
		if c.Name == "keyring" && !strings.Contains(c.Detail, "Headless Linux keyring") {
			t.Fatalf("%+v", c)
		}
	}
}

func projectCheck(t *testing.T, in ProjectInput) Check {
	t.Helper()
	checks := Diagnose(context.Background(), DiagnoseInput{Env: func(string) string { return "" }, Store: Store{Dir: t.TempDir()}, Ref: "x", Now: time.Now, Project: &in})
	last := checks[len(checks)-1]
	if last.Name != "project" {
		t.Fatalf("project check is not last: %+v", checks)
	}
	return last
}

func TestDiagnoseProjectSkipped(t *testing.T) {
	loaded := false
	load := func(context.Context) (map[string]any, error) { loaded = true; return nil, nil }
	if c := projectCheck(t, ProjectInput{Load: load}); c.Status != "skipped" || !strings.Contains(c.Detail, "no project selected") {
		t.Fatalf("%+v", c)
	}
	if c := projectCheck(t, ProjectInput{Selected: "infra", Source: "flag", AuthFailed: true, Load: load}); c.Status != "skipped" || !strings.Contains(c.Detail, "identity check (users/me) failed") {
		t.Fatalf("%+v", c)
	}
	if loaded {
		t.Fatal("skipped check read the project")
	}
}

func TestDiagnoseProjectReportsAccess(t *testing.T) {
	project := map[string]any{"id": json.Number("37"), "slug": "infra-2025", "i_am_member": true, "i_am_admin": false,
		"my_permissions":     []any{"view_us", "add_us", "modify_us", "comment_us", "view_tasks", "add_task", "modify_task", "view_project"},
		"is_epics_activated": false, "is_kanban_activated": true, "is_backlog_activated": true,
		"swimlanes": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}}
	c := projectCheck(t, ProjectInput{Selected: "infra-2025", Source: "env:TAIGA_PROJECT", Load: func(context.Context) (map[string]any, error) { return project, nil }})
	if c.Status != "ok" {
		t.Fatalf("%+v", c)
	}
	for _, want := range []string{"id=37", "slug=infra-2025", "source=env:TAIGA_PROJECT", "member=true", "admin=false",
		"missing permissions: modify_epic, admin_project_values", "epics=off", "kanban=on", "backlog=on", "swimlanes=2"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail lacks %q: %s", want, c.Detail)
		}
	}
	b, _ := json.Marshal(c.Data)
	if !strings.Contains(string(b), `"missing_permissions":["modify_epic","admin_project_values"]`) || !strings.Contains(string(b), `"swimlanes":2`) || !strings.Contains(string(b), `"is_member":true`) {
		t.Fatalf("data %s", b)
	}
	// A project without swimlanes answers null; all permissions present.
	project["swimlanes"] = nil
	project["my_permissions"] = []any{"view_us", "add_us", "modify_us", "comment_us", "view_tasks", "add_task", "modify_task", "modify_epic", "admin_project_values"}
	c = projectCheck(t, ProjectInput{Selected: "37", Source: "flag", Load: func(context.Context) (map[string]any, error) { return project, nil }})
	if c.Status != "ok" || !strings.Contains(c.Detail, "swimlanes=0") || strings.Contains(c.Detail, "missing") {
		t.Fatalf("%+v", c)
	}
}

func TestDiagnoseProjectFailures(t *testing.T) {
	for code, want := range map[string]string{
		"not_found": "taiga project list",
		"forbidden": "member",
		"network":   "network",
	} {
		load := func(context.Context) (map[string]any, error) {
			return nil, &output.Error{Code: code, Cause: "boom", Exit: output.ExitNotFound}
		}
		c := projectCheck(t, ProjectInput{Selected: "infra", Source: "flag", Load: load})
		if c.Status != "failed" || !strings.Contains(c.Detail, code+": boom") || !strings.Contains(c.Detail, want) {
			t.Fatalf("%s: %+v", code, c)
		}
	}
}
