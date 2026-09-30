package auth

import (
	"context"
	"strings"
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
