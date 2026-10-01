package cli

import (
	"strings"
	"testing"
)

// A write answered with a 2xx status whose body is cut short was applied. It must take the
// same path as an answer that does not decode (re-read, postcondition, write_applied), never
// network_error (exit 7), which scripts treat as safe to repeat.

func TestTruncatedValuesAnswerRunsThePostcondition(t *testing.T) {
	// The reviewer's case: someone writes between our read and our PATCH, which overwrites
	// their key; the answer is lost. The re-read shows the version jump: exit 4, applied.
	f, calls := fieldFake(t)
	f.truncate = true
	f.onValues = func(method string, v map[string]any) {
		if method == "PATCH" {
			v["attributes_values"] = map[string]any{"27": true, "999": "orphan", "28": "2027-01-01"}
			v["version"] = 20
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 4 || !strings.Contains(stderr, "field_values_postcondition_failed") || !strings.Contains(stderr, "the re-read after it has version 21") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}

	// Without a concurrent write the re-read confirms ours.
	f, _ = fieldFake(t)
	f.truncate = true
	if out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x"); code != 0 || !strings.Contains(out, `"version": 20`) {
		t.Fatalf("%d %s %s", code, stderr, out)
	}

	// The re-read fails too: write_applied, exit 1.
	f, calls = fieldFake(t)
	f.truncate = true
	f.onValues = func(method string, _ map[string]any) {
		if method == "PATCH" {
			f.fail["GET "+values6808] = 403
		}
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestTruncatedStoryAnswerIsRereadOrWriteApplied(t *testing.T) {
	f, calls := newStoryFake(t)
	f.truncate = true
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--append-description", "fim")
	if code != 0 || !strings.Contains(out, `"description": "fim"`) || len(writes(calls)) != 1 {
		t.Fatalf("update: %d %s %s", code, stderr, out)
	}
	f.fail["GET userstories/6808"] = 403
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--append-description", "de novo")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 2 {
		t.Fatalf("update, re-read fails: %d %s", code, stderr)
	}
	// A created story cannot be re-read without its id: applied, exit 1, not repeated.
	_, stderr, code = runIn(t, f.env(), "", "story", "create", "--subject", "Nova")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 3 {
		t.Fatalf("create: %d %s", code, stderr)
	}
}

func TestTruncatedAssigneesAnswerRunsThePostcondition(t *testing.T) {
	f, calls := newStoryFake(t)
	f.acceptStale, f.truncate = true, true
	f.onPatch = func(s map[string]any) { s["version"] = s["version"].(int) + 1; f.onPatch = nil }
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--add-assignee", "svc")
	if code != 4 || !strings.Contains(stderr, `"code": "assignees_postcondition_failed"`) || !strings.Contains(stderr, "re-read") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
	if _, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--remove-assignee", "svc"); code != 0 {
		t.Fatalf("no race: %d %s", code, stderr)
	}
}

func TestTruncatedFieldCreateIsWriteApplied(t *testing.T) {
	f, calls := fieldFake(t)
	f.truncate = true
	_, stderr, code := runIn(t, f.env(), "", "field", "create", "--kind", "story", "--name", "Nova", "--type", "text")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestTruncatedAPIWriteIsWriteApplied(t *testing.T) {
	f, calls := newStoryFake(t)
	f.truncate = true
	_, stderr, code := runIn(t, f.env(), "", "api", "POST", "userstories", "-F", "project=37", "-f", "subject=x")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}
