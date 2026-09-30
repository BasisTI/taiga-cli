package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type recorded struct {
	method, path, query, auth string
	body                      map[string]any
}

func fakeTaiga(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		calls = append(calls, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), body})
		mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func runIn(t *testing.T, env map[string]string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	if env == nil {
		env = map[string]string{}
	}
	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	a := &App{In: strings.NewReader(stdin), Out: &out, Err: &errOut, Env: func(k string) string { return env[k] }, Cwd: t.TempDir()}
	code := a.Run(args)
	return out.String(), errOut.String(), code
}

func TestAPIGetWithQuery(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[{"id":1}]`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	out, errOut, code := runIn(t, env, "", "api", "GET", "userstories", "--query", "project=37")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	c := (*calls)[0]
	if c.path != "/api/v1/userstories" || c.query != "project=37" || c.auth != "Bearer tok" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(out, `"id": 1`) {
		t.Fatalf("out = %s", out)
	}
}

func TestAPIFieldTypingAndControlChars(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	weird := "linha1\nlinha2\t\u0001 \"aspas\" ação"
	_, errOut, code := runIn(t, env, "", "api", "POST", "userstories",
		"--field", "project=37", "--field", "tags=[\"a\",\"b\"]", "--field", "subject="+weird, "--raw-field", "ref=12", "--field", "note=12 abc")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	b := (*calls)[0].body
	if b["project"] != 37.0 || b["subject"] != weird || b["ref"] != "12" || b["note"] != "12 abc" || len(b["tags"].([]any)) != 2 {
		t.Fatalf("body = %#v", b)
	}
}

func TestAPIDeleteNeedsConfirmation(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	_, errOut, code := runIn(t, env, "", "api", "DELETE", "userstories/1")
	if code != 2 || len(*calls) != 0 || !strings.Contains(errOut, "delete_not_confirmed") {
		t.Fatalf("code=%d calls=%d err=%s", code, len(*calls), errOut)
	}
	_, _, code = runIn(t, env, "", "api", "DELETE", "userstories/1", "--confirm-delete")
	if code != 0 || len(*calls) != 1 {
		t.Fatalf("confirmed delete: code=%d calls=%d", code, len(*calls))
	}
}

func TestAPIDryRunAutoVersionDoesNotWrite(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":7,"subject":"x"}`))
	})
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	out, errOut, code := runIn(t, env, "", "api", "PATCH", "userstories/1", "--field", "comment=oi", "--auto-version", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, c := range *calls {
		if c.method != "GET" {
			t.Fatalf("dry-run sent %s", c.method)
		}
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	body := got["body"].(map[string]any)
	if got["dry_run"] != true || body["version"] != 7.0 || body["comment"] != "oi" {
		t.Fatalf("out = %s", out)
	}
}

func TestAPIMapsHTTPErrorsToExitCodes(t *testing.T) {
	srv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"_error_message":"No UserStory matches"}`))
	})
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	_, errOut, code := runIn(t, env, "", "api", "GET", "userstories/999")
	if code != 5 || !strings.Contains(errOut, `"code": "not_found"`) || !strings.Contains(errOut, "No UserStory matches") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestAPIWithoutURLIsUsageError(t *testing.T) {
	_, errOut, code := runIn(t, map[string]string{"TAIGA_TOKEN": "t"}, "", "api", "GET", "projects")
	if code != 2 || !strings.Contains(errOut, "config_no_url") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestRunNeverExitsZeroAfterError(t *testing.T) {
	var out, errOut bytes.Buffer
	a := &App{In: strings.NewReader(""), Out: &out, Err: &errOut, Env: func(string) string { return "" }}
	a.ran = true
	// An error built without Exit must not turn into exit 0.
	code := a.finish(&output.Error{Code: "boom", Cause: "no exit set"})
	if code != output.ExitUnexpected {
		t.Fatalf("code = %d, want %d", code, output.ExitUnexpected)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("err = %s", errOut.String())
	}
}

func TestAPIUsageErrorsHaveNoSource(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "https://t.example", "TAIGA_TOKEN": "tok"}
	for _, args := range [][]string{
		{"api", "DELETE", "userstories/1"},
		{"api", "POST", "userstories", "--paginate"},
		{"api", "GET", "userstories", "--query", "novalue"},
		{"api", "POST", "userstories", "--field", "=x"},
		{"api", "POST", "userstories", "--input", "/nonexistent/body.json"},
	} {
		_, errOut, code := runIn(t, env, "", args...)
		if code != 2 || strings.Contains(errOut, `"source"`) {
			t.Fatalf("%v: code=%d err=%s", args, code, errOut)
		}
	}
}
