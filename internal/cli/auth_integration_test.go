//go:build integration

package cli

import (
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestIntegrationLoginApiRefreshLogout(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir()}
	_, errOut, code := runIn(t, env, testtaiga.ServicePassword+"\n", "auth", "login", "--url", testtaiga.URL(), "--username", testtaiga.ServiceUser, "--password-stdin", "--insecure-storage")
	if code != 0 {
		t.Fatalf("login: %d %s", code, errOut)
	}
	out, errOut, code := runIn(t, env, "", "api", "GET", "users/me")
	if code != 0 || !strings.Contains(out, testtaiga.ServiceUser) {
		t.Fatalf("api via session: %d %s %s", code, out, errOut)
	}
	if _, errOut, code = runIn(t, env, "", "auth", "refresh"); code != 0 {
		t.Fatalf("refresh: %d %s", code, errOut)
	}
	if _, errOut, code = runIn(t, env, "", "auth", "logout"); code != 0 {
		t.Fatalf("logout: %d %s", code, errOut)
	}
	// with the file secret gone and no session, api must fail with an auth error
	_, _, code = runIn(t, env, "", "api", "GET", "users/me")
	if code != 3 {
		t.Fatalf("after logout exit = %d, want 3", code)
	}
}
