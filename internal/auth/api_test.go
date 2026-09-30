package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestLoginAndRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/auth":
			if body["type"] != "normal" || body["password"] != "pw" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"_error_message":"bad"}`))
				return
			}
			_, _ = w.Write([]byte(`{"auth_token":"a1","refresh":"r1","id":166,"username":"svc"}`))
		case "/api/v1/auth/refresh":
			if body["refresh"] != "r1" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"auth_token":"a2","refresh":"r2"}`))
		}
	}))
	defer srv.Close()
	res, err := Login(context.Background(), srv.Client(), srv.URL, "svc", []byte("pw"))
	if err != nil || res.AuthToken != "a1" || res.UserID != 166 {
		t.Fatalf("%+v %v", res, err)
	}
	_, err = Login(context.Background(), srv.Client(), srv.URL, "svc", []byte("no"))
	if e := output.AsError(err); e.Code != "auth_invalid_credentials" || e.Exit != 3 {
		t.Fatalf("%+v", e)
	}
	a, r, err := RefreshToken(context.Background(), srv.Client(), srv.URL, "r1")
	if err != nil || a != "a2" || r != "r2" {
		t.Fatalf("%s %s %v", a, r, err)
	}
	_, _, err = RefreshToken(context.Background(), srv.Client(), srv.URL, "old")
	if e := output.AsError(err); e.Code != "session_expired" {
		t.Fatalf("%+v", e)
	}
}

func TestLoginNeverFollowsRedirects(t *testing.T) {
	var leaked int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&leaked, 1)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if _, err := Login(context.Background(), srv.Client(), srv.URL, "svc", []byte("pw")); err == nil {
		t.Fatal("a redirect must not count as a successful login")
	}
	if _, _, err := RefreshToken(context.Background(), srv.Client(), srv.URL, "r1"); err == nil {
		t.Fatal("a redirect must not count as a successful refresh")
	}
	if atomic.LoadInt32(&leaked) != 0 {
		t.Fatal("credentials were resent to the redirect target")
	}
}
