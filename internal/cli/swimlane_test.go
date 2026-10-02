package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// swimlaneFake is the story fake with two swimlanes (A is the default) and the stories spread
// over them: 246 has none, 247 is in A, 248 in B. The catalog is in board order, as Taiga sends it.
func swimlaneFake(t *testing.T) (*storyFake, *[]recorded) {
	f, calls := newStoryFake(t)
	f.swimlanes = []map[string]any{
		{"id": 21, "name": "A", "order": 10, "project": 37, "statuses": []any{}},
		{"id": 22, "name": "B", "order": 20, "project": 37, "statuses": []any{}},
	}
	f.defaultSwimlane = 21
	f.stories[6809]["swimlane"] = 21
	f.stories[6810]["swimlane"] = 22
	return f, calls
}

func TestSwimlaneListMarksDefault(t *testing.T) {
	f, calls := swimlaneFake(t)
	out, stderr, code := runIn(t, f.env(), "", "swimlane", "list")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var lanes []map[string]any
	if err := json.Unmarshal([]byte(out), &lanes); err != nil {
		t.Fatal(err)
	}
	if len(lanes) != 2 || lanes[0]["name"] != "A" || lanes[0]["is_default"] != true || lanes[1]["name"] != "B" || lanes[1]["is_default"] != false {
		t.Fatalf("%s", out)
	}
	if _, ok := lanes[0]["statuses"]; !ok {
		t.Fatalf("JSON must keep the API keys: %s", out)
	}
	text, _, code := runIn(t, f.env(), "", "swimlane", "list", "--output", "text")
	if code != 0 || strings.Join(strings.Fields(text), " ") != "id: 21 name: A order: 10 is_default: true id: 22 name: B order: 20 is_default: false" {
		t.Fatalf("%d %q", code, text)
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("list wrote: %+v", writes(calls))
	}
}

func TestSwimlaneListEmptyProject(t *testing.T) {
	f, _ := newStoryFake(t)
	f.swimlanes = []map[string]any{}
	out, stderr, code := runIn(t, f.env(), "", "swimlane", "list")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "list", "--swimlane", "A")
	if code != 5 || !strings.Contains(stderr, `"not_found"`) {
		t.Fatalf("--swimlane without swimlanes: %d %s", code, stderr)
	}
}

func TestSwimlaneTextEscapesControls(t *testing.T) {
	f, _ := swimlaneFake(t)
	f.swimlanes[1]["name"] = "B\x1b[31m\u202eevil\nid: 1"
	text, stderr, code := runIn(t, f.env(), "", "swimlane", "list", "--output", "text")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if strings.ContainsAny(text, "\x1b\u202e") || !strings.Contains(text, `"B\x1b[31m\u202eevil\nid: 1"`) || strings.Count(text, "\nid:") != 1 {
		t.Fatalf("%q", text)
	}
}

func TestStoryListSwimlaneFiltersLocally(t *testing.T) {
	f, calls := swimlaneFake(t)
	f.swimlanes = append(f.swimlanes, map[string]any{"id": 23, "name": "Dup", "order": 30, "project": 37}, map[string]any{"id": 24, "name": "Dup", "order": 40, "project": 37})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--swimlane", "B"}, "[248]"},
		{[]string{"--swimlane", "21"}, "[247]"},
		{[]string{"--no-swimlane"}, "[246]"},
		{[]string{"--no-swimlane", "--closed=false"}, "[246]"},
		{[]string{"--swimlane", "A", "--closed=false"}, "[]"},
	} {
		got := storyRefsOf(t, f, append([]string{"story", "list"}, tc.args...)...)
		if got != tc.want {
			t.Errorf("%v: got %s want %s", tc.args, got, tc.want)
		}
	}
	// The parameter Taiga honours goes along; the fake ignored it, so the result above is local.
	sent := false
	for _, c := range *calls {
		sent = sent || strings.Contains(c.query, "swimnlane=null")
	}
	if !sent {
		t.Fatalf("--no-swimlane did not send swimnlane=null: %+v", *calls)
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "list", "--swimlane", "Dup")
	if code != 2 || !strings.Contains(stderr, `"ambiguous_name"`) {
		t.Fatalf("dup: %d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "list", "--swimlane", "a")
	if code != 5 || !strings.Contains(stderr, `"not_found"`) {
		t.Fatalf("names are exact: %d %s", code, stderr)
	}
}

func storyRefsOf(t *testing.T, f *storyFake, args ...string) string {
	t.Helper()
	out, stderr, code := runIn(t, f.env(), "", args...)
	if code != 0 {
		t.Fatalf("%v: %d %s", args, code, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	refs := []any{}
	for _, it := range items {
		refs = append(refs, it["ref"])
	}
	b, _ := json.Marshal(refs)
	return string(b)
}

func TestStoryListNoSwimlaneRejectsCombination(t *testing.T) {
	for _, args := range [][]string{
		{"story", "list", "--swimlane", "A", "--no-swimlane"},
		{"story", "list", "--swimlane", " "},
		{"story", "update", "246", "--swimlane", "A", "--clear-swimlane"},
	} {
		_, stderr, code := runIn(t, map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok"}, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestStoryUpdateSwimlane(t *testing.T) {
	f, calls := swimlaneFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--swimlane", "B")
	if code != 0 || !strings.Contains(out, `"swimlane": 22`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || bodyJSON(t, w[0].body) != `{"swimlane":22,"version":7}` {
		t.Fatalf("%+v", w)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--swimlane", "nope")
	if code != 5 || len(writes(calls)) != 1 {
		t.Fatalf("unknown swimlane: %d %s", code, stderr)
	}
}

func TestStoryUpdateClearSwimlane(t *testing.T) {
	f, calls := swimlaneFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "247", "--clear-swimlane", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	var plan struct{ Body map[string]any }
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(plan.Body); string(b) != `{"swimlane":null,"version":2}` {
		t.Fatalf("dry-run body %s", b)
	}
	out, stderr, code = runIn(t, f.env(), "", "story", "update", "247", "--clear-swimlane")
	if code != 0 || !strings.Contains(out, `"swimlane": null`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || bodyJSON(t, w[0].body) != `{"swimlane":null,"version":2}` {
		t.Fatalf("%+v", w)
	}
	// Already without swimlane: nothing to write.
	if _, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--clear-swimlane"); code != 0 || len(writes(calls)) != 1 {
		t.Fatalf("no-op clear wrote: %d %s %+v", code, stderr, writes(calls))
	}
}
