package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type fakeAuth struct {
	srv       *httptest.Server
	logins    int32
	refreshes int32
	valid     sync.Map // refresh tokens currently valid
	exp       time.Time
}

func newFakeAuth(t *testing.T) *fakeAuth {
	f := &fakeAuth{exp: time.Now().Add(24 * time.Hour)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/auth":
			n := atomic.AddInt32(&f.logins, 1)
			if body["password"] != "pw" {
				w.WriteHeader(401)
				return
			}
			rt := "r-login-" + itoa64(int64(n))
			f.valid.Store(rt, true)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": fakeJWT(f.exp), "refresh": rt, "id": 166, "username": "svc"})
		case "/api/v1/auth/refresh":
			if _, ok := f.valid.LoadAndDelete(body["refresh"]); !ok {
				w.WriteHeader(401)
				return
			}
			n := atomic.AddInt32(&f.refreshes, 1)
			time.Sleep(20 * time.Millisecond)
			rt := "r-refresh-" + itoa64(int64(n))
			f.valid.Store(rt, true)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": fakeJWT(f.exp), "refresh": rt})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type staticSecret string

func (staticSecret) Name() string                               { return "test" }
func (s staticSecret) Password(context.Context) ([]byte, error) { return []byte(s), nil }

func newResolver(f *fakeAuth, dir string, secret SecretSource) *Resolver {
	return &Resolver{URL: f.srv.URL, Username: "svc", Env: func(string) string { return "" }, Store: Store{Dir: dir}, Secret: secret, HTTP: f.srv.Client(), Now: time.Now, Warn: func(string, string) {}}
}

func TestTokenEnvWins(t *testing.T) {
	r := &Resolver{Env: func(k string) string {
		return map[string]string{"TAIGA_TOKEN": "x", "TAIGA_TOKEN_TYPE": "Application"}[k]
	}}
	tok, err := r.Token(context.Background())
	if err != nil || tok.Value != "x" || tok.Type != "Application" {
		t.Fatalf("%+v %v", tok, err)
	}
}

func TestTokenLoginsOnceThenUsesCache(t *testing.T) {
	f := newFakeAuth(t)
	r := newResolver(f, t.TempDir(), staticSecret("pw"))
	for i := 0; i < 3; i++ {
		if _, err := r.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}
}

func TestExpiredSessionRefreshesOnceAcrossConcurrentCallers(t *testing.T) {
	f := newFakeAuth(t)
	dir := t.TempDir()
	st := Store{Dir: dir}
	ref := SessionRef(f.srv.URL, "svc")
	f.valid.Store("r0", true)
	_ = st.Save(ref, Session{URL: f.srv.URL, Username: "svc", AuthToken: fakeJWT(time.Now().Add(-time.Hour)), Refresh: "r0", Expiry: time.Now().Add(-time.Hour)})
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := newResolver(f, dir, nil).Token(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.refreshes != 1 || f.logins != 0 {
		t.Fatalf("refreshes=%d logins=%d", f.refreshes, f.logins)
	}
}

func readOnlyStateDir(t *testing.T, sess *Session, ref string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dir := t.TempDir()
	if sess != nil {
		_ = Store{Dir: dir}.Save(ref, *sess)
	}
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	_ = os.Chmod(filepath.Join(dir, "sessions"), 0o500)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "sessions"), 0o700) })
	return dir
}

func TestReadOnlyExpiredSessionWithoutSecretIsSessionExpired(t *testing.T) {
	f := newFakeAuth(t)
	ref := SessionRef(f.srv.URL, "svc")
	f.valid.Store("r0", true)
	dir := readOnlyStateDir(t, &Session{URL: f.srv.URL, Username: "svc", AuthToken: "old", Refresh: "r0", Expiry: time.Now().Add(-time.Hour)}, ref)
	_, err := newResolver(f, dir, nil).Token(context.Background())
	e := output.AsError(err)
	if e.Code != "session_expired" || e.Exit != 3 {
		t.Fatalf("%+v", e)
	}
	if refreshInvalidatesPrevious && f.refreshes != 0 {
		t.Fatal("must not burn the persisted refresh token in read-only mode")
	}
}

func TestReadOnlyWithEnvPasswordLogsInInMemoryAndWarns(t *testing.T) {
	f := newFakeAuth(t)
	dir := readOnlyStateDir(t, nil, "")
	var warned string
	r := newResolver(f, dir, staticSecret("pw"))
	r.Warn = func(code, _ string) { warned = code }
	if _, err := r.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if warned != "session_cache_readonly" || f.logins != 1 {
		t.Fatalf("warned=%q logins=%d", warned, f.logins)
	}
}

func TestNoUsernameIsNoSource(t *testing.T) {
	r := &Resolver{Env: func(string) string { return "" }}
	if output.AsError(func() error { _, err := r.Token(context.Background()); return err }()).Code != "auth_no_source" {
		t.Fatal("want auth_no_source")
	}
}

func TestReadOnlyExpiredSessionRecoverySaysRefreshOutsideSandbox(t *testing.T) {
	f := newFakeAuth(t)
	ref := SessionRef(f.srv.URL, "svc")
	dir := readOnlyStateDir(t, &Session{URL: f.srv.URL, Username: "svc", AuthToken: "old", Refresh: "r0", Expiry: time.Now().Add(-time.Hour)}, ref)
	_, err := newResolver(f, dir, nil).Token(context.Background())
	if e := output.AsError(err); e.Recovery != "run `taiga auth refresh` outside the sandbox" {
		t.Fatalf("%+v", e)
	}
}

func TestReadOnlyExpiredSessionWithPasswordDoesNotBurnRefresh(t *testing.T) {
	f := newFakeAuth(t)
	ref := SessionRef(f.srv.URL, "svc")
	f.valid.Store("r0", true)
	dir := readOnlyStateDir(t, &Session{URL: f.srv.URL, Username: "svc", AuthToken: "old", Refresh: "r0", Expiry: time.Now().Add(-time.Hour)}, ref)
	var warned string
	r := newResolver(f, dir, staticSecret("pw"))
	r.Warn = func(code, _ string) { warned = code }
	if _, err := r.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.refreshes != 0 || f.logins != 1 || warned != "session_cache_readonly" {
		t.Fatalf("refreshes=%d logins=%d warned=%q", f.refreshes, f.logins, warned)
	}
	if _, ok := f.valid.Load("r0"); !ok {
		t.Fatal("persisted refresh token was burnt")
	}
}

func TestForceRefreshDoesNotHideNetworkErrors(t *testing.T) {
	f := newFakeAuth(t)
	dir := t.TempDir()
	ref := SessionRef(f.srv.URL, "svc")
	_ = Store{Dir: dir}.Save(ref, Session{URL: f.srv.URL, Username: "svc", AuthToken: "old", Refresh: "r0", Expiry: time.Now().Add(-time.Hour)})
	r := newResolver(f, dir, nil)
	f.srv.Close()
	_, err := r.ForceRefresh(context.Background())
	if e := output.AsError(err); e.Code != "network_error" || e.Exit != output.ExitNetwork {
		t.Fatalf("%+v", e)
	}
}

func TestForceRefreshOnReadOnlyStoreIsTypedError(t *testing.T) {
	f := newFakeAuth(t)
	dir := readOnlyStateDir(t, nil, "")
	_, err := newResolver(f, dir, staticSecret("pw")).ForceRefresh(context.Background())
	if e := output.AsError(err); e.Code != "session_cache_readonly" || e.Exit != output.ExitAuth {
		t.Fatalf("%+v", e)
	}
}
