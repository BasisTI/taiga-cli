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
// nothing conclusive (network after the connection opened, 5xx, 3xx), the result is always
// task_create_unconfirmed, naming the candidates found in the story (findTask). A re-run is never
// offered: it would create a second task.
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

// findTask names the candidates of a POST tasks whose outcome is unknown: the tasks of story not
// in known (read right before the POST), see unconfirmedCreate.
func (s *Service) findTask(ctx context.Context, story, me, body Object, known map[int64]bool, sendErr error) (Object, error) {
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	items, err := s.list(check, "tasks", url.Values{"user_story": {fmt.Sprint(story["id"])}})
	mine := []Object{}
	for _, o := range items {
		if ID(o["user_story"]) == ID(story["id"]) {
			mine = append(mine, o)
		}
	}
	return nil, unconfirmedCreate(taskKind, "the story", fmt.Sprintf("`taiga task list --story %v`", story["ref"]), mine, err, known, me, body, sendErr)
}

// unconfirmedCreate decides nothing: it is <kind>_create_unconfirmed for a POST whose outcome is
// unknown (network after the connection opened, 5xx, 3xx), naming for inspection the candidates
// among items (listErr when they could not be read): owned by me, with the subject sent, not in
// known (read right before the POST). Even one with every field asked for is not proof that it
// came from this POST (another process of the same account may have created it; custom fields,
// watchers and the like are not comparable), so it is never a success (decision of 2026-10-03,
// option B). An empty list, or one that cannot be read, proves no absence either: the request
// may still land. where names the scope searched; list is the command that shows it.
func unconfirmedCreate(k kind, where, list string, items []Object, listErr error, known map[int64]bool, me, body Object, sendErr error) error {
	inspect := list
	var checked string
	if listErr != nil {
		checked = fmt.Sprintf("the %s of %s could not be read to look for it: %s", k.plural(), where, output.AsError(listErr).Error())
	} else {
		found, gets := []string{}, []string{}
		for _, o := range items {
			if !known[ID(o["id"])] && o["subject"] == body["subject"] && ID(o["owner"]) > 0 && ID(o["owner"]) == ID(me["id"]) {
				found = append(found, fmt.Sprintf("#%v (id %d)", o["ref"], ID(o["id"])))
				gets = append(gets, fmt.Sprintf("`taiga %s get %v`", k.name, o["ref"]))
			}
		}
		if len(gets) > 0 {
			inspect = strings.Join(gets, ", ") + " and " + inspect
		}
		switch len(found) {
		case 0:
			checked = fmt.Sprintf("%s has no new %s of this account with this subject yet, but the request may still be running on the server", where, k.name)
		case 1:
			checked = fmt.Sprintf("%s has one new %s of this account with this subject, %s, but nothing proves it is the one this command created", where, k.name, found[0])
		default:
			checked = fmt.Sprintf("%s has %d new %s of this account with this subject: %s; nothing proves which one, if any, this command created", where, len(found), k.plural(), strings.Join(found, ", "))
		}
	}
	return &output.Error{Code: k.name + "_create_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "POST " + k.base,
		Cause:    fmt.Sprintf("the %s may have been created: POST %s failed (%v) and %s", k.name, k.base, sendErr, checked),
		Recovery: fmt.Sprintf("do not re-run the command blindly: it would create another %s, and a %s not found yet may still be saved; inspect with %s", k.name, k.name, inspect),
		Exit:     output.ExitUnexpected}
}

// plural is the name of many stories or tasks, for messages.
func (k kind) plural() string {
	if k.name == "story" {
		return "stories"
	}
	return k.name + "s"
}
