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
	"github.com/BasisTI/taiga-cli/internal/config"
)

func sessionServer(t *testing.T, token string) (string, *[]recorded) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth":
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": token, "refresh": "r1", "id": 166, "username": "svc"})
		case "/api/v1/users/me":
			_, _ = w.Write([]byte(`{"id":166,"username":"svc"}`))
		}
	})
	return srv.URL, calls
}

func TestAPILogsInWithEnvPasswordAndCachesSession(t *testing.T) {
	tok := testJWT(time.Now().Add(24 * time.Hour))
	url, calls := sessionServer(t, tok)
	env := map[string]string{"HOME": t.TempDir(), "TAIGA_URL": url, "TAIGA_USERNAME": "svc", "TAIGA_PASSWORD": "pw"}
	for i := 0; i < 2; i++ {
		if _, errOut, code := runIn(t, env, "", "api", "GET", "users/me"); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	}
	var logins int
	for _, c := range *calls {
		if c.path == "/api/v1/auth" {
			logins++
		}
		if c.path == "/api/v1/users/me" && c.auth != "Bearer "+tok {
			t.Fatalf("authorization = %q", c.auth)
		}
	}
	if logins != 1 {
		t.Fatalf("logins = %d, want 1 (second run uses the cache)", logins)
	}
}

func TestEnvCredentialsAreNotSentToRepoOnlyURL(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"token":    {"TAIGA_TOKEN": "secret-token"},
		"password": {"TAIGA_USERNAME": "svc", "TAIGA_PASSWORD": "pw"},
	} {
		t.Run(name, func(t *testing.T) {
			url, calls := sessionServer(t, testJWT(time.Now().Add(time.Hour)))
			_, errOut, code := runInRepo(t, env, "url = \""+url+"\"\n", "api", "GET", "users/me", "--output", "json")
			if code != 3 || !strings.Contains(errOut, "auth_untrusted_url") {
				t.Fatalf("code=%d err=%s", code, errOut)
			}
			if len(*calls) != 0 {
				t.Fatalf("credentials sent to %s: %+v", url, *calls)
			}
		})
	}
}

func TestEnvCredentialsAllowedForRepoURLKnownToConfig(t *testing.T) {
	url, _ := sessionServer(t, testJWT(time.Now().Add(time.Hour)))
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "taiga", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	_ = os.WriteFile(cfg, []byte("[[hosts]]\nurl = \""+url+"\"\nusername = \"svc\"\nsecret_source = \"keyring\"\n"), 0o600)
	env := map[string]string{"HOME": home, "TAIGA_TOKEN": "secret-token"}
	if _, errOut, code := runInRepo(t, env, "url = \""+url+"\"\n", "api", "GET", "users/me"); code != 0 {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

// Codex review #4, end to end: file secret, no session, read-only sessions dir.
func TestReadOnlyStateWithoutSessionDoesNotUseStoredFileSecret(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	url, calls := sessionServer(t, testJWT(time.Now().Add(time.Hour)))
	home := t.TempDir()
	ref := auth.SessionRef(url, "svc")
	if err := (auth.FileSecret{Path: filepath.Join(home, ".config", "taiga", "secrets", ref)}).Put([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(filepath.Join(home, ".config", "taiga", "config.toml"), config.File{DefaultHost: url, Hosts: []config.Host{{URL: url, Username: "svc", SecretSource: "file"}}}); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(home, ".local", "state", "taiga", "sessions")
	_ = os.MkdirAll(sessions, 0o700)
	_ = os.Chmod(sessions, 0o500)
	t.Cleanup(func() { _ = os.Chmod(sessions, 0o700) })
	_, errOut, code := runIn(t, map[string]string{"HOME": home}, "", "api", "GET", "users/me", "--output", "json")
	if code != 3 || !strings.Contains(errOut, "session_cache_readonly") || len(*calls) != 0 {
		t.Fatalf("code=%d err=%s calls=%d", code, errOut, len(*calls))
	}
}
