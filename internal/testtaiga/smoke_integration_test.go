//go:build integration

package testtaiga

import (
	"net/http"
	"testing"
)

func TestSmokeServiceAccountSeesAPI(t *testing.T) {
	token, refresh := Login(t, ServiceUser, ServicePassword)
	if token == "" || refresh == "" {
		t.Fatal("empty tokens")
	}
	req, _ := http.NewRequest(http.MethodGet, URL()+"/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("users/me status %d", resp.StatusCode)
	}
}
