package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskRejectsInvalidSelectorsAndConflictingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"task", "get"},
		{"task", "get", "abc"},
		{"task", "get", "250", "--id", "9100"},
		{"task", "list", "--story", "-1"},
		{"task", "list", "--status", ""},
		{"task", "list", "--assignee", " "},
		{"task", "list", "--tag", " "},
		{"task", "create", "--subject", "s"},
		{"task", "create", "--story", "abc", "--subject", "s"},
		{"task", "create", "--story", "246"},
		{"task", "create", "--story", "246", "--subject", " "},
		{"task", "create", "--story", "246", "--subject", "s", "--force-version"},
		{"task", "create", "--story", "246", "--subject", "s", "--due-date", "31/12/2026"},
		{"task", "update", "x", "--subject", "s"},
		{"task", "update", "250", "--assignee", "svc", "--clear-assignee"},
		{"task", "update", "250", "--assignee", ""},
		{"task", "update", "250", "--block", " "},
		{"task", "update", "250", "--block", "x", "--unblock"},
		{"task", "update", "250", "--due-date", "2026-02-30"},
		{"task", "update", "250", "--due-date", "2026-12-31", "--clear-due-date"},
		{"task", "update", "250", "--tag", "a", "--add-tag", "b"},
		{"task", "update", "250", "--add-tag", "A", "--remove-tag", "a"},
		{"task", "update", "250", "--description-file", "x", "--append-description", "y"},
		{"task", "close", "x"},
		{"task", "close", "250", "--status", ""},
	} {
		_, stderr, code := runIn(t, nil, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestTaskGetByRefAndID(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "task", "get", "250")
	if code != 0 || !strings.Contains(out, `"url": "`+f.srv+`/project/infra-2025/task/250"`) || !strings.Contains(out, `"custom_attributes"`) || !strings.Contains(out, `"cli"`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if (*calls)[1].path != "/api/v1/tasks/by_ref" || !strings.Contains((*calls)[1].query, "ref=250") {
		t.Fatalf("by_ref: %+v", (*calls)[1])
	}
	out, _, code = runIn(t, f.env(), "", "task", "get", "--id", "9101", "--output", "text")
	if code != 0 || !strings.Contains(out, "ref:          251\n") || !strings.Contains(out, "user_story:   6809\n") || !strings.Contains(out, "/task/251") {
		t.Fatalf("text: %d %s", code, out)
	}
	if _, stderr, code := runIn(t, f.env(), "", "task", "get", "--id", "9200"); code != 2 || !strings.Contains(stderr, "another project") {
		t.Fatalf("other project: %d %s", code, stderr)
	}
	if _, stderr, code := runIn(t, f.env(), "", "task", "get", "246"); code != 5 {
		t.Fatalf("a story ref is not a task: %d %s", code, stderr)
	}
}

func TestTaskTextEscapesTheSubject(t *testing.T) {
	f, _ := newStoryFake(t)
	f.tasks[9100]["subject"] = "fake\nref: 999\u202e"
	out, _, code := runIn(t, f.env(), "", "task", "get", "250", "--output", "text")
	if code != 0 || strings.Contains(out, "\nref: 999") || strings.Contains(out, "\u202e") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestTaskListByStoryChecksLocally(t *testing.T) {
	f, calls := newStoryFake(t)
	f.tasks[9102] = map[string]any{"id": 9102, "ref": 252, "project": 37, "version": 1, "subject": "Alpha", "user_story": 6809, "status": 12,
		"is_closed": false, "tags": []any{[]any{"go", nil}}, "assigned_to": 6, "owner": 5}
	out, stderr, code := runIn(t, f.env(), "", "task", "list", "--story", "246")
	if code != 0 || !strings.Contains(out, `"ref": 250`) || strings.Contains(out, `"ref": 251`) || strings.Contains(out, `"ref": 252`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	last := (*calls)[len(*calls)-1]
	if last.path != "/api/v1/tasks" || !strings.Contains(last.query, "user_story=6808") {
		t.Fatalf("query: %+v", last)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--status", "In progress"}, "[252]"},
		{[]string{"--assignee", "svc"}, "[252]"},
		{[]string{"--closed"}, "[251]"},
		{[]string{"--closed=false"}, "[250 252]"},
		{[]string{"--tag", "GO"}, "[252]"},
		{[]string{"--search", "alp"}, "[252]"},
	} {
		out, stderr, code := runIn(t, f.env(), "", append([]string{"task", "list"}, c.args...)...)
		var items []map[string]any
		if err := json.Unmarshal([]byte(out), &items); err != nil || code != 0 {
			t.Fatalf("%v: %d %s %s", c.args, code, out, stderr)
		}
		refs := []string{}
		for _, it := range items {
			b, _ := json.Marshal(it["ref"])
			refs = append(refs, string(b))
		}
		if got := "[" + strings.Join(refs, " ") + "]"; got != c.want {
			t.Errorf("%v: %s, want %s", c.args, got, c.want)
		}
	}
}

func TestTaskCreatePostsInTheStoryAndRereads(t *testing.T) {
	f, calls := newStoryFake(t)
	dry, stderr, code := runIn(t, f.env(), "", "task", "create", "--story", "246", "--subject", "Nova", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 || !strings.Contains(dry, `"path": "tasks"`) || !strings.Contains(dry, `"user_story": 6808`) {
		t.Fatalf("dry-run: %d %s %s", code, dry, stderr)
	}
	out, stderr, code := runIn(t, f.env(), "", "task", "create", "--story", "246", "--subject", "Nova", "--tag", "Cli", "--status", "In progress",
		"--assignee", "svc", "--due-date", "2026-12-31")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || w[0].method != "POST" || w[0].path != "/api/v1/tasks" {
		t.Fatalf("%+v", w)
	}
	b, _ := json.Marshal(w[0].body)
	if string(b) != `{"assigned_to":6,"due_date":"2026-12-31","project":37,"status":12,"subject":"Nova","tags":["cli"],"user_story":6808}` {
		t.Fatalf("body %s", b)
	}
	if !strings.Contains(out, `/project/infra-2025/task/301"`) {
		t.Fatalf("out: %s", out)
	}
}

func TestTaskCreateResolvesEverythingBeforeWriting(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, args := range [][]string{
		{"--story", "999", "--subject", "s"},
		{"--story", "246", "--subject", "s", "--assignee", "outsider"},
		{"--story", "246", "--subject", "s", "--status", "Nope"},
	} {
		if _, stderr, code := runIn(t, f.env(), "", append([]string{"task", "create"}, args...)...); code != 5 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("wrote: %+v", writes(calls))
	}
}

// A POST whose answer is lost never exits 7 (repeatable) and never invites a re-run.
func TestTaskCreateLostAnswerIsNotRepeatable(t *testing.T) {
	for _, status := range []int{502, 302} {
		f, calls := newStoryFake(t)
		f.fail["POST tasks"] = status
		_, stderr, code := runIn(t, f.env(), "", "task", "create", "--story", "246", "--subject", "Nova")
		if code != 1 || !strings.Contains(stderr, "task_create_unconfirmed") || !strings.Contains(stderr, "taiga task list --story 246") || len(writes(calls)) != 1 {
			t.Fatalf("%d: %d %s", status, code, stderr)
		}
	}
}

func TestTaskUpdateSendsVersionAndDependentFields(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "task", "update", "250", "--block", "esperando", "--assignee", "svc", "--due-date", "2026-12-31", "--add-tag", "Go")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	b, _ := json.Marshal(w[0].body)
	if len(w) != 1 || w[0].path != "/api/v1/tasks/9100" ||
		string(b) != `{"assigned_to":6,"blocked_note":"esperando","due_date":"2026-12-31","is_blocked":true,"tags":["cli","go"],"version":4}` {
		t.Fatalf("%+v %s", w, b)
	}
	if !strings.Contains(out, `"version": 5`) {
		t.Fatalf("out: %s", out)
	}
	_, stderr, code = runIn(t, f.env(), "", "task", "update", "250", "--unblock", "--clear-assignee", "--clear-due-date")
	w = writes(calls)
	b, _ = json.Marshal(w[1].body)
	if code != 0 || string(b) != `{"assigned_to":null,"blocked_note":"","due_date":null,"is_blocked":false,"version":5}` {
		t.Fatalf("%d %s %s", code, b, stderr)
	}
	// Nothing to change: no PATCH.
	if _, _, code := runIn(t, f.env(), "", "task", "update", "250", "--unblock", "--clear-assignee"); code != 0 || len(writes(calls)) != 2 {
		t.Fatalf("no-op wrote: %d %+v", code, writes(calls))
	}
}

func TestTaskUpdateAssigneeMustBeMember(t *testing.T) {
	f, calls := newStoryFake(t)
	if _, stderr, code := runIn(t, f.env(), "", "task", "update", "250", "--assignee", "outsider"); code != 5 || !strings.Contains(stderr, "not a member") {
		t.Fatalf("%d %s", code, stderr)
	}
	if len(writes(calls)) != 0 {
		t.Fatal("wrote")
	}
}

func TestTaskUpdateLostAnswerIsNotRepeatable(t *testing.T) {
	for _, status := range []int{502, 302} {
		f, calls := newStoryFake(t)
		f.fail["PATCH tasks/9100"] = status
		_, stderr, code := runIn(t, f.env(), "", "task", "update", "250", "--append-description", "mais")
		if code != 1 || !strings.Contains(stderr, "task_update_unconfirmed") || !strings.Contains(stderr, "taiga task get 250") || len(writes(calls)) != 1 {
			t.Fatalf("%d: %d %s", status, code, stderr)
		}
	}
}

func TestTaskUpdateConflictOnTheSameField(t *testing.T) {
	f, calls := newStoryFake(t)
	f.onPatch = func(s map[string]any) {
		if s["id"] == 9100 && s["version"] == 4 {
			s["assigned_to"], s["version"] = 6, 5
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "task", "update", "250", "--clear-assignee")
	if code != 4 || !strings.Contains(stderr, "version_conflict") || !strings.Contains(stderr, "assigned_to") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s %+v", code, stderr, writes(calls))
	}
}

func TestTaskCloseOnlyChangesTheStatus(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "task", "close", "250")
	w := writes(calls)
	b, _ := json.Marshal(w)
	if code != 0 || len(w) != 1 || len(w[0].body) != 2 || w[0].body["status"] != float64(13) || !strings.Contains(out, `"is_closed"`) {
		t.Fatalf("%d %s %s %s", code, b, out, stderr)
	}
	if _, _, code := runIn(t, f.env(), "", "task", "close", "251"); code != 0 || len(writes(calls)) != 1 {
		t.Fatalf("already closed wrote: %d", code)
	}
	if _, stderr, code := runIn(t, f.env(), "", "task", "close", "250", "--status", "New"); code != 2 {
		t.Fatalf("open status: %d %s", code, stderr)
	}
}
