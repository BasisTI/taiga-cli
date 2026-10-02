package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/taiga"
)

const gitlabUser = "gitlab-75a8ccb9ff104f85988c6be846d37aa5"

// history_userstory.json is GET history/userstory/<id> from the local Taiga (newest first):
// human comments by admin and by svc, a whitespace comment, an edited and deleted comment, a
// diff without comment, and the two comments Taiga's GitLab integration wrote for a push hook
// (commit mention and status change). Photo URLs and gravatar ids are sanitized.
func historyFixture(t *testing.T) []Object {
	t.Helper()
	b, err := os.ReadFile("testdata/history_userstory.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	out := []Object{}
	for _, o := range raw {
		out = append(out, Object(o))
	}
	return out
}

func TestSystemCommentOnTheLocalFixture(t *testing.T) {
	want := map[string]bool{}
	for _, o := range historyFixture(t) {
		user := o["user"].(map[string]any)
		if SystemComment(o) {
			want[o["comment"].(string)[:20]] = true
			if user["username"] != gitlabUser {
				t.Fatalf("hidden comment of %v: %v", user["username"], o["comment"])
			}
		} else if user["username"] == gitlabUser {
			t.Fatalf("GitLab integration comment kept: %v", o["comment"])
		}
	}
	if len(want) != 2 || !want["This user story has "] || !want["Dev Exemplo changed "] {
		t.Fatalf("hidden: %v", want)
	}
}

func TestSystemCommentConservative(t *testing.T) {
	mention := "This user story has been mentioned by Dev in the [GitLab commit](https://gitlab.example/c/1 \"See commit\") \"x TG-1\""
	status := "Dev changed the status from [GitLab commit](https://gitlab.example/c/1 \"See commit\")\n\n  - Status: **New** → **Done**"
	for _, tc := range []struct {
		name, author string
		active       any
		body         string
		want         bool
	}{
		{"mention", gitlabUser, false, mention, true},
		{"status", gitlabUser, false, status, true},
		{"simple status", gitlabUser, false, "Changed status from GitLab commit.\n\n - Status: **New** → **Done**", true},
		{"simple mention", gitlabUser, false, "This issue has been mentioned in the GitLab commit \"x TG-1\"", true},
		{"human text by the integration user", gitlabUser, false, "Manual review completed", false},
		{"service account", "cedric.integration", true, "Decisão do PO: aceitar o risco", false},
		{"human quoting the template", "alice", true, mention, false},
		{"active gitlab user", gitlabUser, true, mention, false},
		{"no is_active", gitlabUser, nil, mention, false},
		{"name only looks like gitlab", "gitlab-bot", false, mention, false},
		{"uppercase hash", "gitlab-75A8CCB9FF104F85988C6BE846D37AA5", false, mention, false},
		{"no author", "", false, "Unknown integration text", false},
		{"mention without commit link", gitlabUser, false, "This user story has been mentioned by Dev somewhere", false},
	} {
		user := map[string]any{"username": tc.author}
		if tc.active != nil {
			user["is_active"] = tc.active
		}
		o := Object{"user": user, "comment": tc.body, "type": 1}
		if got := SystemComment(o); got != tc.want {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
	if SystemComment(Object{"comment": mention}) || SystemComment(Object{"user": "x", "comment": mention}) {
		t.Fatal("entry without a user object hidden")
	}
}

// Only a connection that never opened proves that nothing reached Taiga.
func TestNotSent(t *testing.T) {
	wrap := func(err error) error {
		return &taiga.NetworkError{Method: "PATCH", Path: "userstories/1", Err: &url.Error{Op: "Patch", URL: "http://x", Err: err}}
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"refused", wrap(&net.OpError{Op: "dial", Err: errors.New("connection refused")}), true},
		{"dns", wrap(&net.DNSError{Err: "no such host", Name: "x"}), true},
		{"reset while reading", wrap(&net.OpError{Op: "read", Err: errors.New("connection reset")}), false},
		{"timeout", wrap(errors.New("context deadline exceeded")), false},
		{"5xx", &taiga.APIError{Status: 503, Method: "PATCH", Path: "userstories/1"}, false},
		{"dial outside a network error", &net.OpError{Op: "dial", Err: errors.New("x")}, false},
	} {
		if got := taiga.NotSent(tc.err); got != tc.want {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
}
