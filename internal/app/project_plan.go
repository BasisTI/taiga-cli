package app

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
)

// Action is one semantic step of a project plan; new statuses have no id or version yet.
type Action struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Body Object `json:"body"`
}

// ProjectPlan is what apply would do. Unmanaged lists what exists and the file does not
// declare (kept as is); Drift lists declared definitions that exist with other values, which
// apply refuses instead of updating.
type ProjectPlan struct {
	Actions   []Action `json:"actions"`
	Unmanaged []Object `json:"unmanaged"`
	Drift     []Object `json:"drift"`
}

// SortStatuses orders statuses as Taiga does (order, then name), numerically.
func SortStatuses(statuses []Object) {
	sort.SliceStable(statuses, func(i, j int) bool {
		a, b := ID(statuses[i]["order"]), ID(statuses[j]["order"])
		if a != b {
			return a < b
		}
		return fmt.Sprint(statuses[i]["name"]) < fmt.Sprint(statuses[j]["name"])
	})
}

// indexNames indexes a catalog of kind ("status" or "field") by name. Two entries with the same
// name are refused: the plan never picks one of them.
func indexNames(items []Object, kind string) (map[string]Object, error) {
	out := map[string]Object{}
	for _, item := range items {
		name, ok := item["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("a %s returned by Taiga has no name: %v", kind, item["id"])
		}
		if _, exists := out[name]; exists {
			return nil, &output.Error{Code: "ambiguous_name", Cause: fmt.Sprintf("Taiga has two %ss named %q", kind, name),
				Recovery: "rename or remove the duplicate in Taiga; nothing was changed", Exit: output.ExitUsage}
		}
		out[name] = item
	}
	return out, nil
}

// caseVariant returns an entry whose name equals name except for case: Taiga accepts both, but
// a second status or field differing only by case is almost certainly a mistake.
func caseVariant(items []Object, name string) Object {
	for _, item := range items {
		if other := fmt.Sprint(item["name"]); other != name && strings.EqualFold(other, name) {
			return item
		}
	}
	return nil
}

// BuildProjectPlan compares spec with the statuses (already sorted by SortStatuses) and story
// field definitions read from Taiga. It never plans an update or a removal.
func BuildProjectPlan(spec ProjectSpec, statuses, fields []Object) (ProjectPlan, error) {
	plan := ProjectPlan{Actions: []Action{}, Unmanaged: []Object{}, Drift: []Object{}}
	sts, err := indexNames(statuses, "status")
	if err != nil {
		return plan, err
	}
	fs, err := indexNames(fields, "field")
	if err != nil {
		return plan, err
	}
	current := []string{}
	for _, st := range statuses {
		current = append(current, fmt.Sprint(st["name"]))
	}
	order, err := OrderStatuses(current, spec.StoryStatus)
	if err != nil {
		return plan, err
	}
	wantedSt, wantedF := map[string]bool{}, map[string]bool{}
	projected := append([]string{}, current...)
	for _, st := range spec.StoryStatus {
		wantedSt[st.Name] = true
		body := Object{"name": st.Name, "color": st.Color, "is_closed": st.Closed}
		old, found := sts[st.Name]
		if !found {
			projected = append(projected, st.Name)
		}
		switch {
		case !found && caseVariant(statuses, st.Name) != nil:
			plan.Drift = append(plan.Drift, drift("status", st.Name, caseVariant(statuses, st.Name), body, []string{"name"}))
		case !found:
			plan.Actions = append(plan.Actions, Action{"create_status", st.Name, body})
		default:
			diff := []string{}
			if color, _ := old["color"].(string); !strings.EqualFold(color, st.Color) {
				diff = append(diff, "color")
			}
			if old["is_closed"] != st.Closed {
				diff = append(diff, "is_closed")
			}
			if len(diff) > 0 {
				plan.Drift = append(plan.Drift, drift("status", st.Name, old, body, diff))
			}
		}
	}
	for _, f := range spec.StoryField {
		wantedF[f.Name] = true
		body := Object{"name": f.Name, "type": f.Type, "description": f.Description}
		old, found := fs[f.Name]
		switch {
		case !found && caseVariant(fields, f.Name) != nil:
			plan.Drift = append(plan.Drift, drift("field", f.Name, caseVariant(fields, f.Name), body, []string{"name"}))
		case !found:
			plan.Actions = append(plan.Actions, Action{"create_field", f.Name, body})
		default:
			diff := []string{}
			if old["type"] != f.Type {
				diff = append(diff, "type")
			}
			if fmt.Sprint(old["description"]) != f.Description {
				diff = append(diff, "description")
			}
			if len(diff) > 0 {
				plan.Drift = append(plan.Drift, drift("field", f.Name, old, body, diff))
			}
		}
	}
	// New statuses are created at the end, in file order; anything else is a reorder.
	if !reflect.DeepEqual(projected, order) {
		plan.Actions = append(plan.Actions, Action{"reorder_statuses", "", Object{"names": order}})
	}
	for _, st := range statuses {
		if !wantedSt[fmt.Sprint(st["name"])] {
			plan.Unmanaged = append(plan.Unmanaged, Object{"kind": "status", "value": st})
		}
	}
	for _, f := range fields {
		if !wantedF[fmt.Sprint(f["name"])] {
			plan.Unmanaged = append(plan.Unmanaged, Object{"kind": "field", "value": f})
		}
	}
	return plan, nil
}

func drift(kind, name string, actual, desired Object, fields []string) Object {
	return Object{"kind": kind, "name": name, "fields": fields, "actual": actual, "desired": desired}
}
