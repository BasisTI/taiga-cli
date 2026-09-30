package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Patch records which changes were asked for; absent fields stay untouched.
type Patch struct {
	Set                 Object
	AddTags, RemoveTags []string
	Append              *string
}

// Names normalizes Taiga tags ([name, color] pairs or plain names) to names.
func Names(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid tags array")
	}
	out := []string{}
	for _, x := range xs {
		if pair, ok := x.([]any); ok {
			if len(pair) == 0 {
				return nil, fmt.Errorf("empty tag pair")
			}
			x = pair[0]
		}
		n, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("invalid tag name")
		}
		out = append(out, n)
	}
	return out, nil
}

// TagName is how Taiga stores a tag: lower case (observed in the local probe).
func TagName(s string) string { return strings.ToLower(s) }

// MergeNames returns current ∪ add − remove, keeping order and dropping duplicates.
func MergeNames(current, add, remove []string) []string {
	removed, seen := map[string]bool{}, map[string]bool{}
	for _, x := range remove {
		removed[x] = true
	}
	out := []string{}
	for _, xs := range [][]string{current, add} {
		for _, x := range xs {
			if !removed[x] && !seen[x] {
				out = append(out, x)
				seen[x] = true
			}
		}
	}
	return out
}

func equal(a, b any) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && string(x) == string(y)
}

// BuildPatch returns only the fields that differ from before.
func BuildPatch(before Object, p Patch) (Object, error) {
	out := Object{}
	for k, v := range p.Set {
		old := before[k]
		if k == "tags" {
			var err error
			old, err = Names(before[k])
			if err != nil {
				return nil, err
			}
		}
		if !equal(old, v) {
			out[k] = v
		}
	}
	if p.Append != nil {
		old, _ := before["description"].(string)
		next := old
		if old != "" && *p.Append != "" {
			next += "\n\n"
		}
		next += *p.Append
		if next != old {
			out["description"] = next
		}
	}
	if len(p.AddTags)+len(p.RemoveTags) > 0 {
		old, err := Names(before["tags"])
		if err != nil {
			return nil, err
		}
		next := MergeNames(old, p.AddTags, p.RemoveTags)
		if !equal(old, next) {
			out["tags"] = next
		}
	}
	return out, nil
}
