package auth

import (
	"context"
	"testing"
	"time"
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
