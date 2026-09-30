package taiga

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
