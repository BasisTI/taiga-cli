//go:build integration

// Package testtaiga holds helpers for integration tests against the local Taiga from compose.test.yml.
package testtaiga

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
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
// URL is not on loopback, so every helper consumer is safe: tests must never write
// to a real Taiga.
func URL() string {
	u := "http://localhost:8000"
	if v := os.Getenv("TAIGA_TEST_URL"); v != "" {
		u = v
	}
	if err := checkLocal(u); err != nil {
		panic(err)
	}
	return u
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
