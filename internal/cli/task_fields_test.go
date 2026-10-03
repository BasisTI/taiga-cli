package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

const values9100 = "tasks/custom-attributes-values/9100"

func TestTaskFieldListAndSet(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "task", "field", "list", "250", "--output", "text")
	if code != 0 || !strings.Contains(out, "Horas (text)") || !strings.Contains(out, "/task/250") {
		t.Fatalf("list: %d %s %s", code, out, stderr)
	}
	out, stderr, code = runIn(t, f.env(), "", "task", "field", "set", "250", "Horas=8h")
	w := writes(calls)
	if code != 0 || len(w) != 1 || w[0].path != "/api/v1/"+values9100 {
		t.Fatalf("set: %d %s %+v", code, stderr, w)
	}
	if b, _ := json.Marshal(w[0].body); string(b) != `{"attributes_values":{"27":"8h"},"version":3}` {
		t.Fatalf("body %s", b)
	}
	if !strings.Contains(out, `"version": 4`) || !strings.Contains(out, `"value": "8h"`) {
		t.Fatalf("out: %s", out)
	}
	// The same value again writes nothing.
	if _, _, code := runIn(t, f.env(), "", "task", "field", "set", "250", "Horas=8h"); code != 0 || len(writes(calls)) != 1 {
		t.Fatalf("no-op wrote: %d", code)
	}
	if _, stderr, code := runIn(t, f.env(), "", "task", "field", "set", "250", "--unset", "Horas"); code != 2 || !strings.Contains(stderr, "only checkbox and date") {
		t.Fatalf("unset text: %d %s", code, stderr)
	}
}

func TestTaskFieldSetPostconditionNamesTheTaskCommand(t *testing.T) {
	f, calls := fieldFake(t)
	f.onPatchValues = func(v map[string]any) {
		if v["task"] == 9100 {
			v["version"] = v["version"].(int) + 1 // someone else wrote right after
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "task", "field", "set", "250", "Horas=8h")
	if code != 4 || !strings.Contains(stderr, "field_values_postcondition_failed") || !strings.Contains(stderr, "taiga task field list 250") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

// A values PATCH of a task whose answer is lost is decided by the re-read, never exit 7.
func TestTaskFieldSetLostAnswer(t *testing.T) {
	for _, status := range []int{502, 302} {
		f, calls := fieldFake(t)
		f.valuesStatus = status
		out, stderr, code := runIn(t, f.env(), "", "task", "field", "set", "250", "Horas=8h")
		if code != 0 || !strings.Contains(out, `"8h"`) || len(writes(calls)) != 1 {
			t.Fatalf("%d applied: %d %s %s", status, code, out, stderr)
		}
		f, calls = fieldFake(t)
		f.fail["PATCH "+values9100] = status
		_, stderr, code = runIn(t, f.env(), "", "task", "field", "set", "250", "Horas=8h")
		if code != 1 || !strings.Contains(stderr, "task_update_unconfirmed") || !strings.Contains(stderr, "taiga task field list 250") || len(writes(calls)) != 1 {
			t.Fatalf("%d not applied: %d %s", status, code, stderr)
		}
	}
}

func TestTaskCommentAndComments(t *testing.T) {
	f, calls := commentFake(t)
	f.history[9100] = []map[string]any{}
	dry, stderr, code := runIn(t, f.env(), "", "task", "comment", "250", "--body", "feito", "--dry-run")
	if code != 0 || !strings.Contains(dry, `"path": "tasks/9100"`) || len(writes(calls)) != 0 {
		t.Fatalf("dry-run: %d %s %s", code, dry, stderr)
	}
	out, stderr, code := runIn(t, f.env(), "", "task", "comment", "250", "--body", "feito")
	w := writes(calls)
	if code != 0 || len(w) != 1 || w[0].path != "/api/v1/tasks/9100" || w[0].body["comment"] != "feito" || w[0].body["version"] != float64(4) ||
		!strings.Contains(out, "/task/250") {
		t.Fatalf("comment: %d %s %s %+v", code, out, stderr, w)
	}
	out, stderr, code = runIn(t, f.env(), "", "task", "comments", "250")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || code != 0 || len(items) != 1 || items[0]["comment"] != "feito" ||
		items[0]["task_ref"] != float64(250) || !strings.HasSuffix(items[0]["url"].(string), "/task/250") {
		t.Fatalf("comments: %d %s %s", code, out, stderr)
	}
	if (*calls)[len(*calls)-1].path != "/api/v1/history/task/9100" {
		t.Fatalf("history path: %+v", (*calls)[len(*calls)-1])
	}
}

func TestTaskCommentLostAnswer(t *testing.T) {
	for _, status := range []int{502, 302} {
		for _, applied := range []bool{true, false} {
			f, calls := commentFake(t)
			f.history[9100] = []map[string]any{}
			f.commentStatus, f.commentApplied = status, applied
			_, stderr, code := runIn(t, f.env(), "", "task", "comment", "250", "--body", "feito")
			want := 0
			if !applied {
				want = 1
			}
			if code != want || len(writes(calls)) != 1 || !applied && (!strings.Contains(stderr, "comment_unconfirmed") || !strings.Contains(stderr, "taiga task comments 250")) {
				t.Fatalf("%d applied=%v: %d %s", status, applied, code, stderr)
			}
		}
	}
}
