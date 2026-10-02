package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/auth"
)

func jwtFor(exp time.Time) string { return testJWT(exp) }

func authServer(t *testing.T) string {
	srv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth":
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": jwtFor(time.Now().Add(24 * time.Hour)), "refresh": "r1", "id": 166, "username": "svc"})
		case "/api/v1/users/me":
			_, _ = w.Write([]byte(`{"id":166,"username":"svc","full_name":"Service"}`))
		}
	})
	return srv.URL
}

func TestLoginWithInsecureStorageThenStatus(t *testing.T) {
	url := authServer(t)
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	_, errOut, code := runIn(t, env, "pw\n", "auth", "login", "--url", url, "--username", "svc", "--password-stdin", "--insecure-storage")
	if code != 0 {
		t.Fatalf("login exit %d: %s", code, errOut)
	}
	ref := auth.SessionRef(url, "svc")
	secret := filepath.Join(home, ".config", "taiga", "secrets", ref)
	if info, err := os.Stat(secret); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(home, ".config", "taiga", "config.toml"))
	if !strings.Contains(string(cfg), "secret_source = 'file'") && !strings.Contains(string(cfg), `secret_source = "file"`) {
		t.Fatalf("config: %s", cfg)
	}
	if strings.Contains(string(cfg), "pw") {
		t.Fatal("password leaked into config")
	}
	out, errOut, code := runIn(t, env, "", "auth", "status")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, errOut)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil || st["user"].(map[string]any)["username"] != "svc" {
		t.Fatalf("status: %s", out)
	}
}

func TestLoginWithoutPasswordSourceOffTTYIsUsage(t *testing.T) {
	url := authServer(t)
	_, errOut, code := runIn(t, nil, "", "auth", "login", "--url", url, "--username", "svc")
	if code != 2 || !strings.Contains(errOut, "--password-stdin") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestStatusDiagnoseAlwaysPrintsChecks(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_USERNAME": "svc", "CODEX_SANDBOX_NETWORK_DISABLED": "1"}
	out, _, code := runIn(t, env, "", "auth", "status", "--diagnose")
	if code == 0 {
		t.Fatal("status without session must fail")
	}
	if !strings.Contains(out, `"sandbox"`) || !strings.Contains(out, `"session_cache"`) {
		t.Fatalf("checks missing: %s", out)
	}
}

func TestLogoutRemovesSessionAndFileSecret(t *testing.T) {
	url := authServer(t)
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	runIn(t, env, "pw\n", "auth", "login", "--url", url, "--username", "svc", "--password-stdin", "--insecure-storage")
	_, errOut, code := runIn(t, env, "", "auth", "logout")
	if code != 0 {
		t.Fatalf("logout: %d %s", code, errOut)
	}
	ref := auth.SessionRef(url, "svc")
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "taiga", "sessions", ref+".json")); !os.IsNotExist(err) {
		t.Fatal("session still present")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "taiga", "secrets", ref)); !os.IsNotExist(err) {
		t.Fatal("secret still present")
	}
}

func TestLoginRollsBackSessionWhenKeyringFails(t *testing.T) {
	// godbus reads the process env: point it at nothing so the real keyring is never touched.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	url := authServer(t)
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	_, errOut, code := runIn(t, env, "pw\n", "auth", "login", "--url", url, "--username", "svc", "--password-stdin")
	if code != 3 || !strings.Contains(errOut, "--insecure-storage") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
	ref := auth.SessionRef(url, "svc")
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "taiga", "sessions", ref+".json")); !os.IsNotExist(err) {
		t.Fatal("session kept after the keyring failed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "taiga", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("config written after the keyring failed")
	}
}

func TestLoginRefusesRepoOnlyURL(t *testing.T) {
	url := authServer(t)
	env := map[string]string{"HOME": t.TempDir()}
	_, errOut, code := runInRepo(t, env, "url = \""+url+"\"\n", "auth", "login", "--username", "svc", "--password-stdin", "--insecure-storage", "--output", "json")
	if code != 3 || !strings.Contains(errOut, "auth_untrusted_url") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestLogoutReportsKeyringDeleteFailure(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "taiga", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	_ = os.WriteFile(cfg, []byte("default_host = \"http://127.0.0.1:1\"\n[[hosts]]\nurl = \"http://127.0.0.1:1\"\nusername = \"svc\"\nsecret_source = \"keyring\"\n"), 0o600)
	out, errOut, code := runIn(t, map[string]string{"HOME": home}, "", "auth", "logout", "--output", "json")
	if code != 3 || !strings.Contains(errOut, "keyring") || strings.Contains(out, "logged out") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}
}

func diagnoseChecks(t *testing.T, env map[string]string, args ...string) (map[string]map[string]any, int) {
	t.Helper()
	out, _, code := runIn(t, env, "", append([]string{"auth", "status", "--diagnose"}, args...)...)
	var st struct{ Checks []map[string]any }
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	byName := map[string]map[string]any{}
	for _, c := range st.Checks {
		byName[c["name"].(string)] = c
	}
	return byName, code
}

func TestStatusDiagnoseChecksProject(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	_, url, calls := newCatalogFake(t)
	checks, code := diagnoseChecks(t, catalogEnv(url))
	p := checks["project"]
	if code != 0 || p["status"] != "ok" || !strings.Contains(p["detail"].(string), "source=env:TAIGA_PROJECT") || !strings.Contains(p["detail"].(string), "missing permissions: view_us") {
		t.Fatalf("%d %v", code, p)
	}
	if data, _ := p["data"].(map[string]any); data["slug"] != "infra-2025" || data["is_member"] != true {
		t.Fatalf("%v", p)
	}
	checks, _ = diagnoseChecks(t, catalogEnv(url), "--project", "99")
	if p := checks["project"]; p["status"] != "failed" || !strings.Contains(p["detail"].(string), "not_found") {
		t.Fatalf("%v", p)
	}
	checks, _ = diagnoseChecks(t, map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": "tok"})
	if p := checks["project"]; p["status"] != "skipped" {
		t.Fatalf("%v", p)
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("diagnose wrote: %+v", writes(calls))
	}
	// Without --diagnose the project is not read.
	n := len(*calls)
	if _, _, code := runIn(t, catalogEnv(url), "", "auth", "status"); code != 0 || len(*calls) != n+1 {
		t.Fatalf("status read more than users/me: %d %+v", code, (*calls)[n:])
	}
}

func TestStatusDiagnoseSkipsProjectWhenAuthFails(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_USERNAME": "svc", "TAIGA_PROJECT": "infra"}
	checks, code := diagnoseChecks(t, env)
	if code == 0 || checks["project"]["status"] != "skipped" || checks["project"]["detail"] != "identity check (users/me) failed" {
		t.Fatalf("%d %v", code, checks["project"])
	}
}

func TestStatusDiagnoseTextEscapesProject(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	f, url, _ := newCatalogFake(t)
	f.project["slug"] = "infra\x1b[31m\u202e\ncheck x: ok"
	out, stderr, code := runIn(t, catalogEnv(url), "", "auth", "status", "--diagnose", "--output", "text")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if strings.ContainsAny(out, "\x1b\u202e") || strings.Contains(out, "\ncheck x") || !strings.Contains(out, `\x1b[31m\u202e\ncheck x: ok`) {
		t.Fatalf("%q", out)
	}
}
