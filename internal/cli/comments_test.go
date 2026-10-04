package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// commentFake extends storyFake with the history of story 6808: the local Taiga fixture of
// internal/app (human, service account, edited and deleted, blank, diff only and GitLab
// integration entries), newest first, two per page.
func commentFake(t *testing.T) (*storyFake, *[]recorded) {
	f, calls := newStoryFake(t)
	b, err := os.ReadFile(filepath.Join("..", "app", "testdata", "history_userstory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&entries); err != nil {
		t.Fatal(err)
	}
	f.history = map[int64][]map[string]any{6808: entries, 6809: {}}
	return f, calls
}

// handleComments serves history/userstory/<id> and history/task/<id> and the PATCH of a comment. Taiga accepts a
// comment with any version up to the current one (docs/api-notes.md); commentStatus answers
// the PATCH with that status instead, after storing the comment when commentApplied.
func (f *storyFake) handleComments(w http.ResponseWriter, r *http.Request, path string) bool {
	if f.history == nil {
		return false
	}
	if r.Method == "GET" && (strings.HasPrefix(path, "history/userstory/") || strings.HasPrefix(path, "history/task/")) {
		id, _ := strconv.ParseInt(path[strings.LastIndex(path, "/")+1:], 10, 64)
		entries, ok := f.history[id]
		if !ok {
			w.WriteHeader(404)
			return true
		}
		f.list(w, r, entries)
		if f.afterHistory != nil && r.URL.Query().Get("page") == "" {
			f.afterHistory(id)
		}
		return true
	}
	if r.Method != "PATCH" || f.store(head(path)) == nil {
		return false
	}
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(b))
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	comment, ok := body["comment"].(string)
	if !ok {
		return false
	}
	id, _ := strconv.ParseInt(tail(path), 10, 64)
	s := f.store(head(path))[id]
	if v, _ := strconv.Atoi(fmt.Sprint(body["version"])); v < 1 || v > s["version"].(int) {
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, `{"version":"The version parameter is not valid"}`)
		return true
	}
	if f.commentStatus == 0 || f.commentApplied {
		f.nextID++
		entry := map[string]any{"id": fmt.Sprintf("entry-%d", f.nextID), "type": 1, "created_at": "2026-10-01T12:00:00.000Z",
			"user": map[string]any{"pk": 5, "username": "admin", "name": "admin", "is_active": true}, "comment": comment,
			"diff": map[string]any{}, "delete_comment_date": nil, "edit_comment_date": nil}
		f.history[id] = append([]map[string]any{entry}, f.history[id]...)
		s["version"] = s["version"].(int) + 1
	}
	if f.afterComment != nil {
		f.afterComment()
	}
	switch {
	case f.commentStatus != 0:
		w.WriteHeader(f.commentStatus)
		_, _ = fmt.Fprint(w, f.commentAnswer)
	case f.truncate:
		cut(w, 200)
	default:
		f.write(w, s)
	}
	return true
}

func commentIDs(t *testing.T, out string) []string {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ids := []string{}
	for _, it := range items {
		ids = append(ids, fmt.Sprint(it["id"]))
	}
	return ids
}

func TestStoryCommentsGolden(t *testing.T) {
	f, calls := commentFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "comments", "246")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "comments.json", strings.ReplaceAll(out, f.srv, "http://taiga.test"))
	text, _, code := runIn(t, f.env(), "", "story", "comments", "246", "--output", "text")
	if code != 0 {
		t.Fatal(code)
	}
	golden(t, "comments.txt", strings.ReplaceAll(text, f.srv, "http://taiga.test"))
	f.history[6808][0]["comment"] = "fake\u2028id: forged\u202eevil"
	text, _, _ = runIn(t, f.env(), "", "story", "comments", "246", "--output", "text")
	if !strings.Contains(text, `"fake\u2028id: forged\u202eevil"`) {
		t.Fatalf("separator or bidi override not quoted:\n%s", text)
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("list wrote: %+v", writes(calls))
	}
	for _, c := range *calls {
		if strings.HasPrefix(c.path, "/api/v1/history/") && c.query != "type=comment" && !strings.HasPrefix(c.query, "page=") {
			t.Fatalf("history query: %s", c.query)
		}
	}
}

// Default output keeps humans, the service account, edited and deleted comments and the comment
// of spaces only (Taiga stores it), and drops the diff-only entry; --include-system adds exactly the two GitLab comments.
func TestStoryCommentsIncludeSystemAddsOnlyTheHidden(t *testing.T) {
	f, _ := commentFake(t)
	out, _, _ := runIn(t, f.env(), "", "story", "comments", "246")
	all, _, code := runIn(t, f.env(), "", "story", "comments", "246", "--include-system")
	if code != 0 {
		t.Fatal(code)
	}
	visible, every := commentIDs(t, out), commentIDs(t, all)
	if len(visible) != 5 || len(every) != 7 {
		t.Fatalf("visible %v, all %v", visible, every)
	}
	seen := map[string]bool{}
	for _, id := range visible {
		seen[id] = true
	}
	var items []map[string]any
	_ = json.Unmarshal([]byte(all), &items)
	for i, it := range items {
		user := it["user"].(map[string]any)
		system := strings.HasPrefix(fmt.Sprint(user["username"]), "gitlab-")
		if it["is_system"] != system || seen[every[i]] == system {
			t.Fatalf("entry %d: %v", i, it)
		}
	}
	if fmt.Sprint(every) != "[1d01a92c-bd8d-11f1-9937-3a8627fadd1b f1eec8c8-bd8c-11f1-9937-3a8627fadd1b f1dd4abc-bd8c-11f1-9937-3a8627fadd1b d60d938c-bd8c-11f1-aef4-3a8627fadd1b d5d3e7ae-bd8c-11f1-a529-3a8627fadd1b d5af4ca0-bd8c-11f1-aef4-3a8627fadd1b d5744e20-bd8c-11f1-a529-3a8627fadd1b]" {
		t.Fatalf("order changed: %v", every)
	}
}

func TestStoryCommentsEmptyIsAnArray(t *testing.T) {
	f, _ := commentFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "comments", "247")
	if code != 0 || out != "[]\n" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
}

func TestStoryCommentPublishesOnceWithTheVersion(t *testing.T) {
	text := "Decisão: \"aspas\"\n\n- item `code`\r\n\tfim ç"
	for _, input := range [][]string{{"--body", text}, {"--body-file", "-"}, {"--body-file", "FILE"}} {
		f, calls := commentFake(t)
		if input[1] == "FILE" {
			input[1] = filepath.Join(t.TempDir(), "c.md")
			if err := os.WriteFile(input[1], []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		out, stderr, code := runIn(t, f.env(), text, append([]string{"story", "comment", "246"}, input...)...)
		if code != 0 {
			t.Fatalf("%v: %d %s", input, code, stderr)
		}
		w := writes(calls)
		if len(w) != 1 || w[0].path != "/api/v1/userstories/6808" || len(w[0].body) != 2 || w[0].body["comment"] != text || w[0].body["version"] != float64(7) {
			t.Fatalf("%v: %+v", input, w)
		}
		var view map[string]any
		if err := json.Unmarshal([]byte(out), &view); err != nil {
			t.Fatal(err)
		}
		if view["version"] != float64(8) || view["ref"] != float64(246) || view["description"] != "" || view["url"] != f.srv+"/project/infra-2025/us/246" {
			t.Fatalf("%v: %s", input, out)
		}
		if got := f.history[6808][0]["comment"]; got != text {
			t.Fatalf("%v: stored %q", input, got)
		}
	}
}

func TestStoryCommentDryRunGolden(t *testing.T) {
	f, calls := commentFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "texto \"x\"", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "comment_dry_run.json", out)
	if len(f.history[6808]) != 8 {
		t.Fatal("dry-run wrote a comment")
	}
}

func TestStoryCommentRejectsBadInputBeforeNetwork(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.md")
	for _, tc := range []struct {
		args  []string
		stdin string
	}{
		{[]string{"246"}, ""},
		{[]string{"246", "--body", "a", "--body-file", "-"}, ""},
		{[]string{"246", "--body", ""}, ""},
		{[]string{"246", "--body", " \n\t"}, ""},
		{[]string{"246", "--body-file", "-"}, "  \n"},
		{[]string{"246", "--body-file", missing}, ""},
		{[]string{"246", "--body-file", "-"}, "r\xe9sum\xe9"},
		{[]string{"0", "--body", "a"}, ""},
		{[]string{"abc", "--body", "a"}, ""},
	} {
		_, stderr, code := runIn(t, map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "p"}, tc.stdin, append([]string{"story", "comment"}, tc.args...)...)
		if code != 2 || !strings.Contains(stderr, `"usage"`) {
			t.Fatalf("%v: %d %s", tc.args, code, stderr)
		}
	}
	for _, args := range [][]string{{"story", "comments", "0"}, {"story", "comments"}, {"story", "comments", "x"}} {
		_, stderr, code := runIn(t, map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "p"}, "", args...)
		if code != 2 || !strings.Contains(stderr, `"usage"`) {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

// A 5xx leaves the outcome unknown. The PATCH is never repeated; the history decides, and a
// comment missing from it is still unconfirmed (exit 1), never a repeatable exit 7.
func TestStoryCommentServerErrorIsNeverRepeated(t *testing.T) {
	f, calls := commentFake(t)
	f.commentStatus, f.commentAnswer = 503, `{"_error_message":"busy"}`
	_, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "primeiro \"aspas\" acentuação\nlinha 2 **md**")
	if code != 1 || !strings.Contains(stderr, `"comment_unconfirmed"`) || !strings.Contains(stderr, "may still be running") || !strings.Contains(stderr, "taiga story comments 246") {
		t.Fatalf("%d %s", code, stderr)
	}
	if len(writes(calls)) != 1 {
		t.Fatalf("repeated: %+v", writes(calls))
	}

	// Applied before the 5xx: the new entry is named (the older one with the same text, already
	// in the fixture, is not), never adopted (option B), and the PATCH is not repeated.
	f, calls = commentFake(t)
	f.commentStatus, f.commentApplied, f.commentAnswer = 502, true, `<html>bad gateway</html>`
	out, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "primeiro \"aspas\" acentuação\nlinha 2 **md**")
	if code != 1 || out != "" || !strings.Contains(stderr, `"comment_unconfirmed"`) || !strings.Contains(stderr, "1 new comment(s) of this account with this text (entry-") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s %s %+v", code, out, stderr, writes(calls))
	}
}

// The review case: a gateway answers 503 while the PATCH is still running upstream, and the
// comment lands only after the CLI checked the history. A script that repeats exit 7 must not
// publish a second copy.
func TestStoryCommentLandingAfterTheCheckIsNotRepeatable(t *testing.T) {
	f, calls := commentFake(t)
	f.commentStatus, f.commentAnswer = 503, `{"_error_message":"gateway timed out; upstream still running"}`
	body := "late " + strings.Repeat("x", 3)
	reads := 0
	f.afterHistory = func(id int64) {
		if reads++; reads == 2 { // the check after the PATCH: the upstream write lands now
			f.history[id] = append([]map[string]any{{"id": "late-entry", "type": 1, "comment": body,
				"user": map[string]any{"pk": 5, "username": "admin", "is_active": true}}}, f.history[id]...)
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		_, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", body)
		if code != 7 {
			if code != 1 || !strings.Contains(stderr, `"comment_unconfirmed"`) {
				t.Fatalf("%d %s", code, stderr)
			}
			break
		}
	}
	copies := 0
	for _, e := range f.history[6808] {
		if e["comment"] == body {
			copies++
		}
	}
	if copies != 1 || len(writes(calls)) != 1 {
		t.Fatalf("copies %d, writes %d", copies, len(writes(calls)))
	}
}

func TestStoryCommentDryRunTextEscapesFormatCharacters(t *testing.T) {
	f, _ := commentFake(t)
	f.defs = map[string][]map[string]any{"userstory-custom-attributes": {}}
	for _, args := range [][]string{
		{"story", "comment", "246", "--body", "abc\u202eevil \u2066x\U000e0041"},
		{"story", "create", "--subject", "abc\u202eevil \u2066x\U000e0041"},
		{"field", "create", "--kind", "story", "--name", "abc\u202eevil \u2066x\U000e0041", "--type", "text"},
	} {
		out, stderr, code := runIn(t, f.env(), "", append(args, "--dry-run", "--output", "text")...)
		if code != 0 || strings.ContainsAny(out, "\u202e\u2066\U000e0041") || !strings.Contains(out, `abc\u202eevil \u2066x\udb40\udc41`) {
			t.Fatalf("%v: %d %s %s", args, code, out, stderr)
		}
	}
}

func TestStoryCommentUnconfirmedWhenTheHistoryCannotBeRead(t *testing.T) {
	f, calls := commentFake(t)
	f.commentStatus, f.commentAnswer = 500, `{}`
	f.afterComment = func() { f.fail["GET history/userstory/6808"] = 404 }
	_, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "novo")
	if code != 1 || !strings.Contains(stderr, `"comment_unconfirmed"`) || !strings.Contains(stderr, "may have been published") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryCommentTruncatedAnswerIsReread(t *testing.T) {
	f, calls := commentFake(t)
	f.truncate = true
	out, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "novo")
	if code != 0 || !strings.Contains(out, `"version": 8`) || len(writes(calls)) != 1 {
		t.Fatalf("%d %s %s", code, out, stderr)
	}

	f, _ = commentFake(t)
	f.truncate = true
	f.afterComment = func() { f.fail["GET userstories/6808"] = 404 }
	_, stderr, code = runIn(t, f.env(), "", "story", "comment", "246", "--body", "novo")
	if code != 1 || !strings.Contains(stderr, `"write_applied"`) {
		t.Fatalf("%d %s", code, stderr)
	}
}

// Taiga never refuses an old version for a comment, but if a version error ever comes back it
// is a conflict (exit 4) and is not retried.
func TestStoryCommentVersionRefusalIsNotRetried(t *testing.T) {
	f, calls := commentFake(t)
	f.commentStatus, f.commentAnswer = 400, `{"version":"The version doesn't match with the current one"}`
	_, stderr, code := runIn(t, f.env(), "", "story", "comment", "246", "--body", "novo")
	if code != 4 || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
	f, calls = commentFake(t)
	f.commentStatus, f.commentAnswer = 403, `{"_error_message":"You do not have permission"}`
	_, stderr, code = runIn(t, f.env(), "", "story", "comment", "246", "--body", "novo")
	if code != 6 || len(writes(calls)) != 1 || strings.Contains(stderr, "history") {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryFieldTextEscapesFormatCharacters(t *testing.T) {
	f, _ := fieldFake(t)
	f.values[values6808]["attributes_values"] = map[string]any{"29": "abc\u202eevil"}
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "list", "246", "--output", "text")
	if code != 0 || strings.Contains(out, "\u202e") || !strings.Contains(out, `"abc\u202eevil"`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}
