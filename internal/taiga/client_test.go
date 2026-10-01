package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestDoSendsAuthJSONAndPath(t *testing.T) {
	var gotAuth, gotCT, gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCT, gotPath, gotQuery = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}, WithRetryWait(0))
	resp, err := c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/7", Query: map[string][]string{"project": {"37"}}, Body: map[string]any{"subject": "Ação\u0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotCT != "application/json" || gotPath != "/api/v1/userstories/7" || gotQuery != "project=37" {
		t.Fatalf("auth=%q ct=%q path=%q q=%q", gotAuth, gotCT, gotPath, gotQuery)
	}
	if gotBody["subject"] != "Ação\u0001" || string(resp.Body) != `{"ok":true}` {
		t.Fatalf("body=%v resp=%s", gotBody, resp.Body)
	}
}

func TestDoRetriesGETOn5xxButNotPATCH(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if r.Method == "GET" && n < 3 {
			w.WriteHeader(502)
			return
		}
		if r.Method == "PATCH" {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "t"}, WithRetryWait(0))
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "projects"}); err != nil {
		t.Fatalf("GET should succeed after retries: %v", err)
	}
	atomic.StoreInt32(&calls, 0)
	_, err := c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/1", Body: map[string]any{}})
	if err == nil || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("PATCH must not retry: calls=%d err=%v", atomic.LoadInt32(&calls), err)
	}
}

func TestToOutputMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
		exit   int
	}{
		{401, `{"detail":"x"}`, "auth_rejected", output.ExitAuth},
		{403, `{}`, "forbidden", output.ExitForbidden},
		{404, `{}`, "not_found", output.ExitNotFound},
		{400, `{"version":"The version parameter is not valid"}`, "version_conflict", output.ExitConflict},
		{409, `{}`, "version_conflict", output.ExitConflict},
		{400, `{"subject":["required"]}`, "invalid_request", output.ExitUsage},
		{500, `oops`, "server_error", output.ExitNetwork},
	}
	for _, c := range cases {
		e := ToOutput(&APIError{Status: c.status, Method: "PATCH", Path: "userstories/1", Body: []byte(c.body)})
		if e.Code != c.code || e.Exit != c.exit || e.Stage != "PATCH userstories/1" || e.Cause != c.body {
			t.Fatalf("%d: %+v", c.status, e)
		}
	}
}

func TestDoDoesNotFollowRedirectOnPATCH(t *testing.T) {
	var calls, gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method == "GET" {
			atomic.AddInt32(&gets, 1)
		}
		http.Redirect(w, r, "/api/v1/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "t"}, WithRetryWait(0))
	_, err := c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/1", Body: map[string]any{"a": 1}})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 302 {
		t.Fatalf("want APIError 302, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 || atomic.LoadInt32(&gets) != 0 {
		t.Fatalf("calls=%d gets=%d", calls, gets)
	}
	e := ToOutput(err)
	if e.Code != "unexpected_redirect" || e.Exit != output.ExitNetwork || !strings.Contains(e.Recovery, "canonical https://") {
		t.Fatalf("%+v", e)
	}
}

func TestDoDoesNotFollowRedirectOnGETWithInjectedClient(t *testing.T) {
	var targetCalls int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetCalls, 1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer srv.Close()
	injected := &http.Client{}
	for _, c := range []*Client{
		New(srv.URL, StaticToken{}, WithRetryWait(0)),
		New(srv.URL, StaticToken{}, WithRetryWait(0), WithHTTPClient(injected)),
	} {
		_, err := c.Do(context.Background(), Request{Method: "GET", Path: "projects"})
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != 301 {
			t.Fatalf("want APIError 301, got %v", err)
		}
	}
	if n := atomic.LoadInt32(&targetCalls); n != 0 {
		t.Fatalf("redirect target contacted %d times", n)
	}
	if injected.CheckRedirect != nil {
		t.Fatal("the caller's http.Client must not be mutated")
	}
}

func TestDoGETStopsAfterThreeAttemptsOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "projects"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 503 || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestToOutputVersionConflictRecovery(t *testing.T) {
	for _, body := range []string{`{"version":"The version parameter is not valid"}`, `{"version":"The version doesn't match with the current one"}`} {
		e := ToOutput(&APIError{Status: 400, Method: "PATCH", Path: "userstories/1", Body: []byte(body)})
		if e.Code != "version_conflict" || e.Recovery != "re-read the resource and retry; with `taiga api`, include \"version\" or use --auto-version" {
			t.Fatalf("%+v", e)
		}
	}
}

// truncating answers status with a Content-Length it does not honour: the body is cut and the
// connection closed, as when a proxy or the network drops the answer after the status line.
func truncating(t *testing.T, status int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"attribu`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoWriteWithTruncatedBodyAfter2xxIsConfirmedNotNetwork(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "PUT"} {
		var hits atomic.Int32
		c := New(truncating(t, 200, &hits).URL, StaticToken{}, WithRetryWait(0))
		_, err := c.Do(context.Background(), Request{Method: method, Path: "userstories/custom-attributes-values/7?x=secret", Body: map[string]any{"a": 1}})
		var ue *UnreadableBodyError
		if !errors.As(err, &ue) || ue.Status != 200 || ue.Method != method {
			t.Fatalf("%s: %T %v", method, err, err)
		}
		if hits.Load() != 1 {
			t.Fatalf("%s: write repeated %d times", method, hits.Load())
		}
		e := ToOutput(err)
		if e.Code != "write_applied" || e.Exit != output.ExitUnexpected || strings.Contains(e.Error(), "secret") || !strings.Contains(e.Cause, "HTTP 200") {
			t.Fatalf("%s: %+v", method, e)
		}
	}
}

func TestDoTruncatedBodyOtherwiseStaysANetworkError(t *testing.T) {
	var hits atomic.Int32
	c := New(truncating(t, 200, &hits).URL, StaticToken{}, WithRetryWait(0))
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "userstories/7"})
	var ne *NetworkError
	if !errors.As(err, &ne) || hits.Load() != 3 {
		t.Fatalf("GET: %T %v, %d attempts", err, err, hits.Load())
	}
	hits.Store(0)
	c = New(truncating(t, 400, &hits).URL, StaticToken{}, WithRetryWait(0))
	_, err = c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/7", Body: map[string]any{}})
	if !errors.As(err, &ne) || hits.Load() != 1 {
		t.Fatalf("PATCH 400: %T %v, %d attempts", err, err, hits.Load())
	}
}
