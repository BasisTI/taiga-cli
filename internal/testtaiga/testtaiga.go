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

// URL returns the base URL of the test Taiga (no /api/v1 suffix).
func URL() string {
	if u := os.Getenv("TAIGA_TEST_URL"); u != "" {
		return u
	}
	return "http://localhost:8000"
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
