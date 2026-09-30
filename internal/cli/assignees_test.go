package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func bodyJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStoryAssigneeFlagsRejectedBeforeNetwork(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
	for _, args := range [][]string{
		{"story", "update", "246", "--block", ""},
		{"story", "update", "246", "--block", " \n"},
		{"story", "update", "246", "--block", "x", "--unblock"},
		{"story", "update", "246", "--add-assignee", ""},
		{"story", "update", "246", "--remove-assignee", " "},
		{"story", "update", "246", "--owner-assignee", ""},
		{"story", "update", "246", "--owner-assignee", "svc", "--clear-owner-assignee"},
		{"story", "update", "246", "--add-assignee", "svc", "--remove-assignee", "svc"},
		{"story", "update", "246", "--owner-assignee", "svc", "--remove-assignee", "svc"},
		{"story", "create", "--subject", "s", "--assignee", ""},
		{"story", "create", "--subject", "s", "--block", "x"},
		{"story", "create", "--subject", "s", "--add-assignee", "svc"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestStoryUpdateAssigneesTagsAndBlockDryRun(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--add-tag", "novo", "--add-assignee", "svc", "--add-assignee", "6",
		"--block", "aguardando \"B6\"\nlinha 2", "--dry-run")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var plan struct{ Body map[string]any }
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}
	want := `{"assigned_users":[5,6],"blocked_note":"aguardando \"B6\"\nlinha 2","is_blocked":true,"tags":["cli","go","novo"],"version":7}`
	if got := bodyJSON(t, plan.Body); got != want {
		t.Fatalf("body %s", got)
	}
	if len(writes(calls)) != 0 {
		t.Fatal("dry-run wrote")
	}
}

func TestStoryUpdateAddRemoveAssignees(t *testing.T) {
	f, calls := newStoryFake(t)
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--add-assignee", "svc")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--remove-assignee", "me")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 2 || bodyJSON(t, w[0].body) != `{"assigned_users":[5,6],"version":7}` || bodyJSON(t, w[1].body) != `{"assigned_users":[6],"version":8}` {
		t.Fatalf("%+v", w)
	}
	// Removing the last assignee sends an empty list, not null and not a missing key.
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "248", "--remove-assignee", "svc")
	if w := writes(calls); code != 0 || len(w) != 3 || bodyJSON(t, w[2].body["assigned_users"]) != `[]` {
		t.Fatalf("%d %s %+v", code, stderr, w)
	}
}

func TestStoryUpdateAssigneeAndBlockNoops(t *testing.T) {
	f, calls := newStoryFake(t)
	f.stories[6810]["is_blocked"] = false
	f.stories[6810]["blocked_note"] = ""
	f.stories[6809]["is_blocked"] = true
	f.stories[6809]["blocked_note"] = "nota"
	for _, args := range [][]string{
		{"story", "update", "246", "--add-assignee", "admin", "--add-assignee", "me"},
		{"story", "update", "246", "--remove-assignee", "svc"},
		{"story", "update", "248", "--unblock"},
		{"story", "update", "247", "--block", "nota"},
		{"story", "update", "248", "--clear-owner-assignee"},
	} {
		if _, stderr, code := runIn(t, f.env(), "", args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("no-op wrote: %+v", writes(calls))
	}
}

func TestStoryAssigneeOutsideProjectIsNotFound(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, args := range [][]string{
		{"story", "update", "246", "--add-assignee", "outsider"},
		{"story", "update", "246", "--add-assignee", "9"},
		{"story", "update", "246", "--remove-assignee", "nobody"},
		{"story", "update", "246", "--owner-assignee", "outsider"},
		{"story", "update", "246", "--add-assignee", "svc", "--block", "x", "--add-assignee", "outsider"},
		{"story", "create", "--subject", "s", "--assignee", "outsider"},
	} {
		_, stderr, code := runIn(t, f.env(), "", args...)
		if code != 5 || !strings.Contains(stderr, "not_found") {
			t.Errorf("%v: %d %s", args, code, stderr)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("wrote: %+v", writes(calls))
	}
}

func TestStoryOwnerAssigneeIsExplicit(t *testing.T) {
	f, calls := newStoryFake(t)
	f.stories[6810]["assigned_to"] = 6
	// Taiga keeps showing the owner among the assignees: refusing is honest, a PATCH would lie.
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "248", "--remove-assignee", "svc")
	if code != 2 || !strings.Contains(stderr, "main assignee") || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--owner-assignee", "svc")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "248", "--remove-assignee", "svc", "--clear-owner-assignee")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 2 || bodyJSON(t, w[0].body) != `{"assigned_to":6,"assigned_users":[5,6],"version":7}` ||
		bodyJSON(t, w[1].body) != `{"assigned_to":null,"assigned_users":[],"version":1}` {
		t.Fatalf("%+v", w)
	}
}

func TestStoryCreateWithAssignees(t *testing.T) {
	f, calls := newStoryFake(t)
	_, stderr, code := runIn(t, f.env(), "", "story", "create", "--subject", "Nova", "--assignee", "svc", "--assignee", "me", "--assignee", "6")
	w := writes(calls)
	if code != 0 || len(w) != 1 || bodyJSON(t, w[0].body["assigned_users"]) != `[6,5]` {
		t.Fatalf("%d %s %+v", code, stderr, w)
	}
	if _, ok := w[0].body["assigned_to"]; ok {
		t.Fatal("create set assigned_to")
	}
}

// A concurrent change of a merged field between our read and our PATCH is a conflict with no
// second PATCH; a concurrent change of an unrelated field allows the single guarded retry.
func TestStoryAssigneeAndBlockConcurrentChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		change func(map[string]any)
		exit   int
		writes int
	}{
		{"assignees", []string{"--add-assignee", "svc"}, func(s map[string]any) { s["assigned_users"] = []any{5, 9} }, 4, 1},
		{"blocked_note", []string{"--block", "minha"}, func(s map[string]any) { s["blocked_note"] = "deles" }, 4, 1},
		{"subject only, block", []string{"--block", "minha"}, func(s map[string]any) { s["subject"] = "outro" }, 0, 2},
		// assigned_users hides the stored list, so an unchanged answer proves nothing: no retry.
		{"subject only, assignees", []string{"--add-assignee", "svc", "--block", "minha"}, func(s map[string]any) { s["subject"] = "outro" }, 4, 1},
		{"subject only, owner", []string{"--owner-assignee", "svc"}, func(s map[string]any) { s["subject"] = "outro" }, 4, 1},
		{"forced assignees", []string{"--add-assignee", "svc", "--force-version"}, func(s map[string]any) { s["assigned_users"] = []any{5, 9} }, 0, 2},
	} {
		f, calls := newStoryFake(t)
		f.onPatch = func(s map[string]any) {
			tc.change(s)
			s["version"] = s["version"].(int) + 1
			f.onPatch = nil
		}
		_, stderr, code := runIn(t, f.env(), "", append([]string{"story", "update", "246"}, tc.args...)...)
		if code != tc.exit || len(writes(calls)) != tc.writes {
			t.Errorf("%s: exit %d writes %d %s", tc.name, code, len(writes(calls)), stderr)
		}
		if tc.exit == 4 && !strings.Contains(stderr, "version_conflict") {
			t.Errorf("%s: %s", tc.name, stderr)
		}
	}
}

func TestStoryTextShowsOwnerAssigneesAndBlock(t *testing.T) {
	f, _ := newStoryFake(t)
	f.stories[6810]["assigned_to"] = nil
	f.stories[6810]["is_blocked"] = true
	out, stderr, code := runIn(t, f.env(), "", "story", "get", "248", "--output", "text")
	if code != 0 || strings.Contains(out, "<nil>") || !strings.Contains(out, "assigned_users:  [6]") || !strings.Contains(out, "is_blocked:      true") {
		t.Fatalf("%d %s\n%s", code, stderr, out)
	}
}

// A former member left in assigned_users (or one added through the raw API) must still be removable.
func TestStoryRemoveAssigneeOutsideProject(t *testing.T) {
	f, calls := newStoryFake(t)
	f.stories[6808]["assigned_users"] = []any{5, 9}
	for _, sel := range []string{"outsider", "9"} {
		f.stories[6808]["assigned_users"] = []any{5, 9}
		_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--remove-assignee", sel)
		w := writes(calls)
		if code != 0 || len(w) == 0 || bodyJSON(t, w[len(w)-1].body["assigned_users"]) != `[5]` {
			t.Fatalf("%s: %d %s %+v", sel, code, stderr, w)
		}
	}
}
