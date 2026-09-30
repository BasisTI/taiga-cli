package app

import (
	"fmt"
	"strings"
)

// MergeIDs returns current ∪ add − remove, keeping order and dropping duplicates. It never
// returns nil, so removing the last id sends [].
func MergeIDs(current, add, remove []int64) []int64 {
	removed, seen := map[int64]bool{}, map[int64]bool{}
	for _, id := range remove {
		removed[id] = true
	}
	out := []int64{}
	for _, xs := range [][]int64{current, add} {
		for _, id := range xs {
			if !removed[id] && !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
	}
	return out
}

func validAssignment(p Patch) error {
	removed := map[int64]bool{}
	for _, id := range p.RemoveAssignees {
		if id <= 0 {
			return Usage("assignee must be a positive id")
		}
		removed[id] = true
	}
	for _, id := range p.AddAssignees {
		if id <= 0 {
			return Usage("assignee must be a positive id")
		}
		if removed[id] {
			return Usage(fmt.Sprintf("assignee cannot be added and removed together: %d", id))
		}
	}
	if p.Owner != nil && p.ClearOwner {
		return Usage("choose --owner-assignee or --clear-owner-assignee")
	}
	if p.Owner != nil && (*p.Owner <= 0 || removed[*p.Owner]) {
		return Usage("--owner-assignee must be a positive id that is not removed")
	}
	if p.Block != nil && p.Unblock {
		return Usage("choose --block or --unblock")
	}
	if p.Block != nil && strings.TrimSpace(*p.Block) == "" {
		return Usage("--block requires a note")
	}
	return nil
}

// assignees reads assigned_users as Taiga shows it: the stored list plus assigned_to.
func assignees(v any) ([]int64, error) {
	if v == nil {
		return []int64{}, nil
	}
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid assigned_users response")
	}
	out := []int64{}
	for _, x := range xs {
		id := ID(x)
		if id <= 0 {
			return nil, fmt.Errorf("invalid assignee id in response: %v", x)
		}
		out = append(out, id)
	}
	return out, nil
}

// AssignmentPatch returns the assignee and block fields that differ from before.
//
// Taiga 6.7 answers assigned_users as the stored list plus assigned_to (docs/api-notes.md), so:
// removing the owner without changing assigned_to is refused, because Taiga would keep showing
// them; and any change of assigned_to also sends the full list, because the previous owner may
// not be in the stored list and would silently disappear.
func AssignmentPatch(before Object, p Patch) (Object, error) {
	if err := validAssignment(p); err != nil {
		return nil, err
	}
	out := Object{}
	if len(p.AddAssignees)+len(p.RemoveAssignees) > 0 || p.Owner != nil || p.ClearOwner {
		current, err := assignees(before["assigned_users"])
		if err != nil {
			return nil, err
		}
		owner := ID(before["assigned_to"])
		next := owner
		if p.Owner != nil {
			next = *p.Owner
		}
		if p.ClearOwner {
			next = 0
		}
		for _, id := range p.RemoveAssignees {
			if id == owner && next == owner {
				return nil, Usage(fmt.Sprintf("user %d is the main assignee (assigned_to), which Taiga always shows among the assignees; "+
					"change it with --owner-assignee or --clear-owner-assignee in the same command", id))
			}
		}
		users := MergeIDs(current, p.AddAssignees, p.RemoveAssignees)
		if next > 0 {
			users = MergeIDs(users, []int64{next}, nil)
		}
		if next != owner {
			out["assigned_to"] = nil
			if next > 0 {
				out["assigned_to"] = next
			}
			out["assigned_users"] = users
		} else if !equal(current, users) {
			out["assigned_users"] = users
		}
	}
	// Taiga's OCC protects only the fields a PATCH sends (docs/api-notes.md, "OCC por campo"), so
	// a block change always sends the pair it was computed from.
	if p.Block != nil && (before["is_blocked"] != true || before["blocked_note"] != *p.Block) {
		out["is_blocked"], out["blocked_note"] = true, *p.Block
	}
	if p.Unblock && (before["is_blocked"] != false || before["blocked_note"] != "") {
		out["is_blocked"], out["blocked_note"] = false, ""
	}
	return out, nil
}
