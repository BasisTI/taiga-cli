//go:build integration

// Package testtaiga holds helpers for integration tests against the local Taiga from compose.test.yml.
package testtaiga

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

const (
	AdminUser       = "admin"
	AdminPassword   = "admin123"
	ServiceUser     = "svc"
	ServicePassword = "svc12345"
	ProjectSlug     = "cli-test"
)

// URL returns the base URL of the test Taiga (no /api/v1 suffix). It panics if the
// URL points at a non-local host, so every helper consumer is safe: tests must never
// write to a real Taiga. Set TAIGA_TEST_ALLOW_REMOTE=1 to override.
func URL() string {
	u := "http://localhost:8000"
	if v := os.Getenv("TAIGA_TEST_URL"); v != "" {
		u = v
	}
	if err := checkLocal(u, os.Getenv("TAIGA_TEST_ALLOW_REMOTE") == "1"); err != nil {
		panic(err)
	}
	return u
}

// checkLocal accepts localhost, loopback addresses and single-label hosts (compose service names).
func checkLocal(rawURL string, allowRemote bool) error {
	if allowRemote {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("testtaiga: invalid TAIGA_TEST_URL %q", rawURL)
	}
	host := u.Hostname()
	if host == "localhost" || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("testtaiga: refusing non-local TAIGA_TEST_URL host %q (set TAIGA_TEST_ALLOW_REMOTE=1 to override)", host)
}

// Login authenticates directly against /api/v1/auth, bypassing the code under test.
func Login(t *testing.T, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"type": "normal", "username": username, "password": password})
	resp, err := http.Post(URL()+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var out struct {
		AuthToken string `json:"auth_token"`
		Refresh   string `json:"refresh"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return out.AuthToken, out.Refresh
}
