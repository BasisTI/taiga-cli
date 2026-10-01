package app

import (
	"context"
	"fmt"

	"github.com/BasisTI/taiga-cli/internal/output"
)

// ApplyResult is what project apply did. Applied and Remaining describe a partial run; Complete
// is set only after a new read finds nothing left to do. Requests is the dry-run preview.
type ApplyResult struct {
	Plan      ProjectPlan `json:"plan"`
	Applied   []Action    `json:"applied"`
	Remaining []Action    `json:"remaining"`
	Complete  bool        `json:"complete"`
	Requests  []WritePlan `json:"requests"`
	Deferred  []Action    `json:"deferred"`
}

// StatusWriter is the Taiga side of apply; its contracts are in docs/api-notes.md.
type StatusWriter interface {
	CheckPermission(context.Context) error
	ProjectID() any
	Load(context.Context) (statuses, fields []Object, err error)
	CheckReorderSupport(context.Context) error
	CreateStatus(context.Context, Object) error
	CreateField(context.Context, Object) error
	Reorder(context.Context, []string) error
}

// Apply validates everything (permission, drift, reorder support) before the first write, then
// runs the plan in order. A failure stops it: nothing is undone and nothing is repeated, and
// the result says what was applied and what remains. Running it again re-reads and re-plans.
func Apply(ctx context.Context, writer StatusWriter, spec ProjectSpec, dry bool) (ApplyResult, error) {
	result := ApplyResult{Applied: []Action{}, Remaining: []Action{}, Requests: []WritePlan{}, Deferred: []Action{}}
	if err := writer.CheckPermission(ctx); err != nil {
		return result, err
	}
	statuses, fields, err := writer.Load(ctx)
	if err != nil {
		return result, err
	}
	plan, err := BuildProjectPlan(spec, statuses, fields)
	result.Plan = plan
	result.Remaining = append(result.Remaining, plan.Actions...)
	if err != nil {
		return result, err
	}
	if len(plan.Drift) > 0 {
		return result, &output.Error{Code: "definition_drift", Cause: fmt.Sprintf("%d declared definitions differ from Taiga", len(plan.Drift)),
			Recovery: "review the drift in `taiga project plan`; fix the file or the project, apply never updates an existing definition", Exit: output.ExitUsage}
	}
	for _, action := range plan.Actions {
		if action.Kind == "reorder_statuses" {
			if err := writer.CheckReorderSupport(ctx); err != nil {
				return result, err
			}
		}
	}
	result.Requests, result.Deferred, err = PreviewProject(plan, statuses, writer.ProjectID())
	if err != nil {
		return result, err
	}
	if dry {
		return result, nil
	}
	for i, action := range plan.Actions {
		switch action.Kind {
		case "create_status":
			err = writer.CreateStatus(ctx, action.Body)
		case "create_field":
			err = writer.CreateField(ctx, action.Body)
		case "reorder_statuses":
			names, ok := action.Body["names"].([]string)
			if !ok {
				return result, fmt.Errorf("invalid reorder plan")
			}
			err = writer.Reorder(ctx, names)
		default:
			return result, fmt.Errorf("unknown project action: %s", action.Kind)
		}
		if err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, action)
		result.Remaining = append([]Action{}, plan.Actions[i+1:]...)
	}
	statuses, fields, err = writer.Load(ctx)
	if err != nil {
		return result, err
	}
	after, err := BuildProjectPlan(spec, statuses, fields)
	if err != nil {
		return result, err
	}
	result.Remaining = after.Actions
	result.Complete = len(after.Actions) == 0 && len(after.Drift) == 0
	if !result.Complete {
		return result, &output.Error{Code: "project_changed", Cause: "the project changed during apply: a new read still finds work or drift",
			Recovery: "run `taiga project plan` again and review it before applying", Exit: output.ExitConflict}
	}
	return result, nil
}

// nextStatusOrder is the order that puts a new status after every current one. Taiga creates a
// status without order at 10, which is not the end of every board (docs/api-notes.md).
func nextStatusOrder(statuses []Object) int64 {
	var max int64
	for _, st := range statuses {
		if n := ID(st["order"]); n > max {
			max = n
		}
	}
	return max + 1
}

const statusOrderPath = "userstory-statuses/bulk_update_order"

// orderPairs is the bulk_update_order body for names: [id, position] with 1-based positions.
// missing reports a name without a status in index.
func orderPairs(names []string, index map[string]Object) ([][]int64, bool) {
	pairs := [][]int64{}
	for i, name := range names {
		st, ok := index[name]
		if !ok {
			return nil, true
		}
		pairs = append(pairs, []int64{ID(st["id"]), int64(i + 1)})
	}
	return pairs, false
}

// PreviewProject materializes the requests of plan without sending anything. statuses is the
// read that produced plan. New statuses get an order after the current ones, in file order.
func PreviewProject(plan ProjectPlan, statuses []Object, projectID any) ([]WritePlan, []Action, error) {
	requests, deferred := []WritePlan{}, []Action{}
	index, err := indexNames(statuses, "status")
	if err != nil {
		return nil, nil, err
	}
	next := nextStatusOrder(statuses)
	for _, action := range plan.Actions {
		switch action.Kind {
		case "create_status", "create_field":
			path := "userstory-statuses"
			if action.Kind == "create_field" {
				path = "userstory-custom-attributes"
			}
			body := Object{"project": projectID}
			for k, v := range action.Body {
				body[k] = v
			}
			if action.Kind == "create_status" {
				body["order"] = next
				next++
			}
			requests = append(requests, WritePlan{true, "POST", path, body})
		case "reorder_statuses":
			names, ok := action.Body["names"].([]string)
			if !ok {
				return nil, nil, fmt.Errorf("invalid reorder plan")
			}
			pairs, missing := orderPairs(names, index)
			if missing {
				// Statuses created by this run have no id yet: the bulk body is known only then.
				deferred = append(deferred, Action{"reorder_statuses", "", Object{"names": names, "depends_on": "create_status"}})
				continue
			}
			requests = append(requests, WritePlan{true, "POST", statusOrderPath, Object{"project": projectID, "bulk_userstory_statuses": pairs}})
		}
	}
	return requests, deferred, nil
}
