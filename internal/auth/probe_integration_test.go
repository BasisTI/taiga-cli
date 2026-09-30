//go:build integration

package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// These probes pin down Taiga 6.7 behaviour that the resolver depends on (spec §10).
func TestProbeTokenLifetimes(t *testing.T) {
	res, err := Login(context.Background(), http.DefaultClient, testtaiga.URL(), testtaiga.ServiceUser, []byte(testtaiga.ServicePassword))
	if err != nil {
		t.Fatal(err)
	}
	exp, err := JWTExpiry(res.AuthToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINDING auth_token lifetime ≈ %s", time.Until(exp).Round(time.Minute))
	if rexp, err := JWTExpiry(res.Refresh); err == nil {
		t.Logf("FINDING refresh lifetime ≈ %s", time.Until(rexp).Round(time.Hour))
	}
}

// Conservative expectation: after a refresh, the previous refresh token is rejected.
// If this test fails, set refreshInvalidatesPrevious = false in resolver.go and flip the assertion.
func TestProbeRefreshRotationInvalidatesPrevious(t *testing.T) {
	ctx := context.Background()
	res, err := Login(ctx, http.DefaultClient, testtaiga.URL(), testtaiga.ServiceUser, []byte(testtaiga.ServicePassword))
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := RefreshToken(ctx, http.DefaultClient, testtaiga.URL(), res.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINDING refresh rotated = %v", r2 != res.Refresh)
	_, _, err = RefreshToken(ctx, http.DefaultClient, testtaiga.URL(), res.Refresh)
	if err == nil {
		t.Fatal("previous refresh token still works: set refreshInvalidatesPrevious = false")
	}
}
