//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestIntegrationSwimlaneListAndUse runs the swimlane commands against a project of its own,
// with two swimlanes created by admin through `taiga api`; svc, a plain member, moves stories.
func TestIntegrationSwimlaneListAndUse(t *testing.T) {
	env, svc, pid := freshProject(t, "cli-test-swimlanes-"+fmt.Sprint(time.Now().UnixNano()))
	out, errOut, code := runIn(t, env, "", "swimlane", "list")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty project: %d %q %s", code, out, errOut)
	}
	if _, errOut, code := runIn(t, env, "", "story", "list", "--swimlane", "A"); code != 5 {
		t.Fatalf("--swimlane without swimlanes: %d %s", code, errOut)
	}

	// Stories exist before the first swimlane, which takes them all (docs/api-notes.md).
	early := storyJSON(t, env, "", "story", "create", "--subject", "before the swimlanes")
	a := storyJSON(t, env, "", "api", "POST", "swimlanes", "-F", "project="+pid, "-f", "name=A")
	b := storyJSON(t, env, "", "api", "POST", "swimlanes", "-F", "project="+pid, "-f", "name=B")
	out, errOut, code = runIn(t, svc, "", "swimlane", "list")
	if code != 0 {
		t.Fatalf("svc swimlane list: %d %s", code, errOut)
	}
	var lanes []map[string]any
	if err := json.Unmarshal([]byte(out), &lanes); err != nil {
		t.Fatal(err)
	}
	if len(lanes) != 2 || lanes[0]["name"] != "A" || lanes[0]["is_default"] != true || lanes[1]["name"] != "B" || lanes[1]["is_default"] != false {
		t.Fatalf("lanes: %s", out)
	}

	inA := storyJSON(t, env, "", "story", "create", "--subject", "in A", "--swimlane", "A")
	none := storyJSON(t, env, "", "story", "create", "--subject", "without swimlane")
	if inA["swimlane"] != a["id"] || none["swimlane"] != nil {
		t.Fatalf("created: A=%v none=%v", inA["swimlane"], none["swimlane"])
	}
	ref := func(o map[string]any) string { return fmt.Sprint(o["ref"]) }
	moved := storyJSON(t, svc, "", "story", "update", ref(inA), "--swimlane", "B")
	if moved["swimlane"] != b["id"] {
		t.Fatalf("update --swimlane B: %v", moved["swimlane"])
	}
	if got := storyRefs(t, env, "--swimlane", "B"); got != ref(inA) {
		t.Fatalf("--swimlane B: %s", got)
	}
	if got := storyRefs(t, env, "--swimlane", fmt.Sprint(a["id"])); got != ref(early) {
		t.Fatalf("--swimlane <A id>: %s", got)
	}
	if got := storyRefs(t, env, "--no-swimlane"); got != ref(none) {
		t.Fatalf("--no-swimlane: %s", got)
	}

	plan := storyJSON(t, svc, "", "story", "update", ref(inA), "--clear-swimlane", "--dry-run")
	if body, _ := json.Marshal(plan["body"]); string(body) != fmt.Sprintf(`{"swimlane":null,"version":%v}`, moved["version"]) {
		t.Fatalf("dry-run: %s", body)
	}
	cleared := storyJSON(t, svc, "", "story", "update", ref(inA), "--clear-swimlane")
	if cleared["swimlane"] != nil || fmt.Sprint(cleared["version"]) == fmt.Sprint(moved["version"]) {
		t.Fatalf("clear: swimlane=%v version=%v", cleared["swimlane"], cleared["version"])
	}
	again := storyJSON(t, svc, "", "story", "update", ref(inA), "--clear-swimlane")
	if fmt.Sprint(again["version"]) != fmt.Sprint(cleared["version"]) {
		t.Fatalf("clearing twice wrote: %v → %v", cleared["version"], again["version"])
	}
	if got := storyRefs(t, env, "--no-swimlane"); got != ref(inA)+","+ref(none) && got != ref(none)+","+ref(inA) {
		t.Fatalf("--no-swimlane after clear: %s", got)
	}
	if got := storyRefs(t, env, "--swimlane", "B"); got != "" {
		t.Fatalf("--swimlane B after clear: %s", got)
	}
}
