package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// TaskView is the output form of a task: tag names instead of pairs, plus the web URL.
func (s *Service) TaskView(o Object) (Object, error) { return s.view(taskKind, o) }

// taskRemoteFilters are the task list filters the probe proved Taiga honours. "ref" is ignored
// by the server; q and tags have other semantics than the local check (full-text search; OR of
// the tags), so they stay local only.
var taskRemoteFilters = map[string]string{"story": "user_story", "status": "status", "assignee": "assigned_to", "closed": "status__is_closed"}

// Tasks lists every task of the project matching filters (keys: story, status, assignee, closed,
// search, tag, ref; ids already resolved). Every filter is checked locally too.
func (s *Service) Tasks(ctx context.Context, filters url.Values) ([]Object, error) {
	return s.items(ctx, taskKind, taskRemoteFilters, filters)
}

// UpdateTask applies p to the task ref. assigned_to is a plain versioned field of a task
// (docs/api-notes.md), so it takes the guarded retry like any other.
func (s *Service) UpdateTask(ctx context.Context, ref string, p Patch, dry, force bool) (any, error) {
	before, err := s.Task(ctx, ref, 0)
	if err != nil {
		return nil, err
	}
	return s.updateFrom(ctx, taskKind, before, p, dry, force)
}

// CloseTask moves the task to a closed status, like CloseStory. It only changes the status: the
// MCP's archived tag is never added.
func (s *Service) CloseTask(ctx context.Context, ref, selector string, dry, force bool) (any, error) {
	return s.close(ctx, taskKind, ref, selector, dry, force)
}

// CreateTask posts body as a task of story, sent once and without version. When the answer says
// nothing conclusive (network after the connection opened, 5xx, 3xx), the tasks of the story are
// searched (findTask); a match is the result, with a warning, otherwise task_create_unconfirmed.
// A re-run is never offered: it would create a second task.
func (s *Service) CreateTask(ctx context.Context, story Object, body Object, dry bool) (any, error) {
	subject, _ := body["subject"].(string)
	if strings.TrimSpace(subject) == "" {
		return nil, Usage("--subject is required")
	}
	body["project"], body["user_story"] = s.Project["id"], story["id"]
	if dry {
		return WritePlan{true, "POST", "tasks", body}, nil
	}
	me, err := Read(ctx, s.API, "users/me", nil)
	if err != nil {
		return nil, err
	}
	known, err := s.storyTasks(ctx, story)
	if err != nil {
		return nil, err
	}
	check := fmt.Sprintf("`taiga task list --story %v`", story["ref"])
	r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: "tasks", Body: body})
	if err != nil {
		var ue *taiga.UnreadableBodyError
		switch {
		case errors.As(err, &ue):
			return nil, taskKind.applied(taiga.ToOutput(err), check)
		case uncertain(err):
			return s.findTask(ctx, story, me, body, known, err)
		}
		return nil, taiga.ToOutput(err)
	}
	created, err := Decode(r.Body)
	if err == nil && ID(created["id"]) <= 0 {
		err = fmt.Errorf("the response has no task id")
	}
	if err != nil {
		return nil, taskKind.applied(WriteApplied("POST", "tasks", r.Status, taiga.ToOutput(fmt.Errorf("decode the created task: %w", err))), check)
	}
	raw, err := reread(ctx, s.API, "POST", "tasks", fmt.Sprintf("tasks/%d", ID(created["id"])), r)
	if err != nil {
		return nil, taskKind.applied(err, check)
	}
	if ID(raw["project"]) != ID(s.Project["id"]) || ID(raw["id"]) != ID(created["id"]) {
		return nil, taskKind.applied(WriteApplied("POST", "tasks", r.Status, fmt.Errorf("the re-read returned another task than id %d", ID(created["id"]))), check)
	}
	return s.TaskView(raw)
}

// storyTasks returns the ids of the tasks of story.
func (s *Service) storyTasks(ctx context.Context, story Object) (map[int64]bool, error) {
	items, err := s.list(ctx, "tasks", url.Values{"user_story": {fmt.Sprint(story["id"])}})
	if err != nil {
		return nil, err
	}
	ids := map[int64]bool{}
	for _, o := range items {
		if ID(o["user_story"]) == ID(story["id"]) {
			ids[ID(o["id"])] = true
		}
	}
	return ids, nil
}

// findTask looks for the task of a POST whose outcome is unknown: a task of story, owned by me,
// with the subject sent, that was not in known (read right before the POST). The only such task
// is the result when, read in full, it holds every field sent; that is a match, not a proof
// (another process of the same account may have created an identical task meanwhile), so a
// warning says so. Any other case is task_create_unconfirmed, naming the candidates for
// inspection.
func (s *Service) findTask(ctx context.Context, story, me, body Object, known map[int64]bool, sendErr error) (Object, error) {
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	items, err := s.list(check, "tasks", url.Values{"user_story": {fmt.Sprint(story["id"])}})
	recovery := fmt.Sprintf("do not re-run the command blindly: it would create another task; wait, check `taiga task list --story %v` and create it only if it is still missing", story["ref"])
	var checked string
	if err != nil {
		checked = "the tasks of the story could not be read to check: " + output.AsError(err).Error()
	} else {
		found := []Object{}
		for _, o := range items {
			if ID(o["user_story"]) == ID(story["id"]) && !known[ID(o["id"])] && o["subject"] == body["subject"] && ID(o["owner"]) > 0 && ID(o["owner"]) == ID(me["id"]) {
				found = append(found, o)
			}
		}
		switch len(found) {
		case 1:
			ref, id := found[0]["ref"], ID(found[0]["id"])
			recovery = fmt.Sprintf("do not re-run the command blindly: it would create another task; inspect the candidate with `taiga task get %v` and `taiga task list --story %v`, and create the task only if it is still missing", ref, story["ref"])
			full, err := Read(check, s.API, fmt.Sprintf("tasks/%d", id), nil)
			if err != nil {
				checked = fmt.Sprintf("the story has one new task of this account with this subject, #%v (id %d), but it could not be read to compare: %s", ref, id, output.AsError(err).Error())
				break
			}
			sent := Object{}
			for k, v := range body {
				if k != "project" {
					sent[k] = v
				}
			}
			if diff := differences(full, sent); len(diff) > 0 {
				checked = fmt.Sprintf("the story has one new task of this account with this subject, #%v (id %d), but its %s differ from the request: it may belong to another process", ref, id, strings.Join(diff, ", "))
				break
			}
			s.Warnings = append(s.Warnings, Warning{Code: "task_create_matched", Message: fmt.Sprintf(
				"the POST answer was lost (%v); task #%v is the story's only new task of this account and holds every field sent, so it is taken as the one created. "+
					"This is a match, not a proof: another process using the same account could have created an identical task", sendErr, ref)})
			return s.TaskView(full)
		case 0:
			checked = "the story has no new task with this subject yet, but the request may still be running on the server"
		default:
			refs := []string{}
			for _, o := range found {
				refs = append(refs, fmt.Sprintf("#%v", o["ref"]))
			}
			checked = fmt.Sprintf("the story has %d new tasks of this account with this subject (%s)", len(found), strings.Join(refs, ", "))
		}
	}
	return nil, &output.Error{Code: "task_create_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "POST tasks",
		Cause:    fmt.Sprintf("the task may have been created: POST tasks failed (%v) and %s", sendErr, checked),
		Recovery: recovery, Exit: output.ExitUnexpected}
}
