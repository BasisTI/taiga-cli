package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestMergeIDsKeepsOrderAndDropsDuplicates(t *testing.T) {
	got := MergeIDs([]int64{6, 166, 6}, []int64{20, 6, 20}, []int64{166})
	if !reflect.DeepEqual(got, []int64{6, 20}) {
		t.Fatalf("%v", got)
	}
	if got := MergeIDs(nil, nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("empty merge must be [] not nil: %#v", got)
	}
}

func TestAssignmentPatchPreservesOwnerAndCurrentAssignees(t *testing.T) {
	before := Object{"assigned_to": json.Number("6"), "assigned_users": []any{json.Number("6"), json.Number("166")}}
	got, err := AssignmentPatch(before, Patch{AddAssignees: []int64{20, 20}, RemoveAssignees: []int64{166}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["assigned_users"], []int64{6, 20}) {
		t.Fatalf("%v", got)
	}
	if _, exists := got["assigned_to"]; exists {
		t.Fatal("implicit owner change")
	}
	if !reflect.DeepEqual(before["assigned_users"], []any{json.Number("6"), json.Number("166")}) {
		t.Fatal("input mutated")
	}
}

func TestAssignmentPatchNoopAndLastAssignee(t *testing.T) {
	before := Object{"assigned_to": nil, "assigned_users": []any{json.Number("6")}}
	got, err := AssignmentPatch(before, Patch{AddAssignees: []int64{6}, RemoveAssignees: []int64{9}})
	if err != nil || len(got) != 0 {
		t.Fatalf("no-op: %v %v", got, err)
	}
	got, err = AssignmentPatch(before, Patch{RemoveAssignees: []int64{6}})
	if err != nil {
		t.Fatal(err)
	}
	if users, ok := got["assigned_users"].([]int64); !ok || users == nil || len(users) != 0 {
		t.Fatalf("last assignee must send []: %#v", got["assigned_users"])
	}
	if got, err := AssignmentPatch(Object{"assigned_users": nil}, Patch{AddAssignees: []int64{5}}); err != nil || !reflect.DeepEqual(got["assigned_users"], []int64{5}) {
		t.Fatalf("null list: %v %v", got, err)
	}
}

// Taiga shows assigned_users as the stored list plus assigned_to (serializer), so removing the
// owner alone is silently ignored; the CLI refuses it instead of reporting a change it did not make.
func TestAssignmentPatchRemovingOwnerNeedsExplicitOwnerChange(t *testing.T) {
	before := Object{"assigned_to": json.Number("6"), "assigned_users": []any{json.Number("5"), json.Number("6")}}
	_, err := AssignmentPatch(before, Patch{RemoveAssignees: []int64{6}})
	if exitOf(err) != 2 || !strings.Contains(err.Error(), "main assignee") {
		t.Fatalf("%v", err)
	}
	got, err := AssignmentPatch(before, Patch{RemoveAssignees: []int64{6}, ClearOwner: true})
	if err != nil || got["assigned_to"] != nil || !reflect.DeepEqual(got["assigned_users"], []int64{5}) {
		t.Fatalf("clear+remove: %v %v", got, err)
	}
	if _, ok := got["assigned_to"]; !ok {
		t.Fatal("clear must send assigned_to null")
	}
	got, err = AssignmentPatch(before, Patch{RemoveAssignees: []int64{6}, Owner: ptr(int64(5))})
	if err != nil || got["assigned_to"] != int64(5) || !reflect.DeepEqual(got["assigned_users"], []int64{5}) {
		t.Fatalf("switch+remove: %v %v", got, err)
	}
}

// Changing assigned_to alone can drop a previous owner that was never in the stored list
// (probe TG-247), so every owner change also sends the full list read before it.
func TestAssignmentPatchOwnerChangeSendsCurrentList(t *testing.T) {
	before := Object{"assigned_to": json.Number("5"), "assigned_users": []any{json.Number("5"), json.Number("6")}}
	got, err := AssignmentPatch(before, Patch{Owner: ptr(int64(6))})
	if err != nil || got["assigned_to"] != int64(6) || !reflect.DeepEqual(got["assigned_users"], []int64{5, 6}) {
		t.Fatalf("switch: %v %v", got, err)
	}
	got, err = AssignmentPatch(before, Patch{ClearOwner: true})
	if err != nil || !reflect.DeepEqual(got["assigned_users"], []int64{5, 6}) {
		t.Fatalf("clear: %v %v", got, err)
	}
	got, err = AssignmentPatch(Object{"assigned_to": nil, "assigned_users": []any{}}, Patch{Owner: ptr(int64(20))})
	if err != nil || got["assigned_to"] != int64(20) || !reflect.DeepEqual(got["assigned_users"], []int64{20}) {
		t.Fatalf("new owner joins the list: %v %v", got, err)
	}
	for _, p := range []Patch{{Owner: ptr(int64(5))}, {ClearOwner: true}} {
		b := Object{"assigned_to": json.Number("5"), "assigned_users": []any{json.Number("5")}}
		if p.ClearOwner {
			b["assigned_to"] = nil
		}
		if got, err := AssignmentPatch(b, p); err != nil || len(got) != 0 {
			t.Fatalf("same owner is a no-op: %v %v", got, err)
		}
	}
}

func TestAssignmentPatchBlockUnblockAndInvalidCombinations(t *testing.T) {
	note := "waiting for B6\n\"review\""
	got, err := AssignmentPatch(Object{"is_blocked": false, "blocked_note": ""}, Patch{Block: &note})
	if err != nil || got["is_blocked"] != true || got["blocked_note"] != note {
		t.Fatalf("%v %v", got, err)
	}
	// Re-blocking with another note also sends is_blocked: a concurrent unblock is then a conflict.
	got, err = AssignmentPatch(Object{"is_blocked": true, "blocked_note": "old"}, Patch{Block: &note})
	if err != nil || len(got) != 2 || got["is_blocked"] != true || got["blocked_note"] != note {
		t.Fatalf("re-block pair: %v %v", got, err)
	}
	got, err = AssignmentPatch(Object{"is_blocked": true, "blocked_note": note}, Patch{Unblock: true})
	if err != nil || got["is_blocked"] != false || got["blocked_note"] != "" {
		t.Fatalf("%v %v", got, err)
	}
	// Taiga's OCC protects only the fields sent: unblocking a story blocked without a note must
	// still send the note, or a concurrent note would be erased without a conflict.
	got, err = AssignmentPatch(Object{"is_blocked": true, "blocked_note": ""}, Patch{Unblock: true})
	if err != nil || len(got) != 2 || got["is_blocked"] != false || got["blocked_note"] != "" {
		t.Fatalf("unblock pair: %v %v", got, err)
	}
	if got, err := AssignmentPatch(Object{"is_blocked": false, "blocked_note": ""}, Patch{Unblock: true}); err != nil || len(got) != 0 {
		t.Fatalf("unblock no-op: %v %v", got, err)
	}
	blank := " \n"
	owner := int64(6)
	for name, p := range map[string]Patch{
		"overlap":         {AddAssignees: []int64{6}, RemoveAssignees: []int64{6}},
		"block+unblock":   {Block: &note, Unblock: true},
		"blank note":      {Block: &blank},
		"owner+clear":     {Owner: &owner, ClearOwner: true},
		"owner removed":   {Owner: &owner, RemoveAssignees: []int64{6}},
		"zero add":        {AddAssignees: []int64{0}},
		"negative remove": {RemoveAssignees: []int64{-1}},
		"zero owner":      {Owner: ptr(int64(0))},
	} {
		if _, err := AssignmentPatch(Object{}, p); exitOf(err) != 2 {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := AssignmentPatch(Object{"assigned_users": "x"}, Patch{AddAssignees: []int64{5}}); err == nil {
		t.Fatal("invalid response accepted")
	}
}

func TestBuildPatchMergesAssignmentsFromTheSameRead(t *testing.T) {
	before := Object{"subject": "a", "tags": []any{[]any{"x", nil}}, "assigned_to": nil, "assigned_users": []any{json.Number("5")}, "is_blocked": false, "blocked_note": ""}
	note := "b"
	got, err := BuildPatch(before, Patch{Set: Object{"subject": "b"}, AddTags: []string{"y"}, AddAssignees: []int64{6}, Block: &note})
	if err != nil {
		t.Fatal(err)
	}
	want := Object{"subject": "b", "tags": []string{"x", "y"}, "assigned_users": []int64{5, 6}, "is_blocked": true, "blocked_note": "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestAssigneeProblems(t *testing.T) {
	n := func(xs ...int) []any {
		out := []any{}
		for _, x := range xs {
			out = append(out, json.Number(fmt.Sprint(x)))
		}
		return out
	}
	base := Object{"assigned_to": json.Number("5"), "assigned_users": n(5, 6)}
	for _, tc := range []struct {
		name  string
		after Object
		p     Patch
		want  string // substring of the problems, "" for none
	}{
		{"ok add", Object{"assigned_to": json.Number("5"), "assigned_users": n(5, 6, 7)}, Patch{AddAssignees: []int64{7}}, ""},
		{"add missing", Object{"assigned_to": json.Number("5"), "assigned_users": n(5, 6)}, Patch{AddAssignees: []int64{7}}, "7 was not added"},
		{"remove kept", Object{"assigned_to": json.Number("6"), "assigned_users": n(5, 6)}, Patch{RemoveAssignees: []int64{6}}, "6 was not removed"},
		{"owner", Object{"assigned_to": json.Number("9"), "assigned_users": n(5, 6, 9)}, Patch{Owner: ptr(int64(6))}, "main assignee is 9, not 6"},
		{"clear", Object{"assigned_to": json.Number("9"), "assigned_users": n(5, 6, 9)}, Patch{ClearOwner: true}, "main assignee is 9, not none"},
		{"owner kept", Object{"assigned_to": nil, "assigned_users": n(5, 6, 7)}, Patch{AddAssignees: []int64{7}}, "main assignee is none, not 5"},
		{"vanished", Object{"assigned_to": json.Number("5"), "assigned_users": n(5, 7)}, Patch{AddAssignees: []int64{7}}, "6 disappeared"},
	} {
		got := strings.Join(assigneeProblems(base, tc.after, tc.p), "; ")
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}
