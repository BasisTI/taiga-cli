package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// gitlabSystemUser is the username Taiga's GitLab integration creates for itself
// (taiga/hooks/gitlab/migrations/0001_initial.py: "gitlab-" + uuid4().hex, inactive, is_system).
var gitlabSystemUser = regexp.MustCompile(`^gitlab-[0-9a-f]{32}$`)

// SystemComment reports a history entry written by Taiga's own GitLab integration for a push
// hook: the inactive gitlab-<hash> user and one of the hook's comment templates
// (taiga/hooks/event_hooks.py). Both must match. A service account, a human quoting the
// template, a diff without text or an unknown author are never system comments: agents
// publish human comments with integration accounts (docs/api-notes.md).
func SystemComment(o Object) bool {
	user, ok := o["user"].(map[string]any)
	if !ok {
		return false
	}
	username, _ := user["username"].(string)
	if active, ok := user["is_active"].(bool); !ok || active || !gitlabSystemUser.MatchString(username) {
		return false
	}
	body, _ := o["comment"].(string)
	switch {
	case strings.HasPrefix(body, "This user story has been mentioned by ") && strings.Contains(body, " in the [GitLab commit]("):
		return true
	case strings.HasPrefix(body, "This issue has been mentioned in the GitLab commit "):
		return true
	case strings.Contains(body, " changed the status from [GitLab commit]("):
		return true
	case strings.HasPrefix(body, "Changed status from GitLab commit."):
		return true
	}
	return false
}

func historyPath(k kind, item Object) string {
	return fmt.Sprintf("history/%s/%d", k.history, ID(item["id"]))
}

// history lists the entries of the story or task that carry a comment, in Taiga's order (newest
// first). type=comment only shortens the list; the text is checked here too. A comment of
// spaces only is kept: Taiga stores it as a comment.
func (s *Service) history(ctx context.Context, k kind, item Object) ([]Object, error) {
	raws, err := s.API.GetAll(ctx, historyPath(k, item), url.Values{"type": {"comment"}})
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	out := []Object{}
	for _, raw := range raws {
		o, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if body, _ := o["comment"].(string); body != "" {
			out = append(out, o)
		}
	}
	return out, nil
}

// Comments lists the comments of a story or task (name), newest first as Taiga sends them.
// Comments of the GitLab integration (SystemComment) are left out unless includeSystem; every
// entry is marked with is_system and its story or task (story_id/story_ref or task_id/task_ref).
// Edited and deleted comments stay, with Taiga's edit_comment_date and delete_comment_date.
func (s *Service) Comments(ctx context.Context, name, ref string, includeSystem bool) ([]Object, error) {
	k, err := kindOf(name)
	if err != nil {
		return nil, err
	}
	item, err := s.byRef(ctx, k.base, k.name, ref, 0)
	if err != nil {
		return nil, err
	}
	view, err := s.view(k, item)
	if err != nil {
		return nil, err
	}
	entries, err := s.history(ctx, k, item)
	if err != nil {
		return nil, err
	}
	out := []Object{}
	for _, o := range entries {
		system := SystemComment(o)
		if system && !includeSystem {
			continue
		}
		o["is_system"] = system
		o[k.name+"_id"], o[k.name+"_ref"], o["url"] = item["id"], item["ref"], view["url"]
		out = append(out, o)
	}
	return out, nil
}

// Comment publishes body on a story or task (name) with PATCH {comment, version} and returns it
// read again. Taiga's OCC never refuses a comment (it is not a field, so an old version is
// accepted; docs/api-notes.md), so the version protects nothing and the PATCH is sent once,
// never repeated. When its outcome is unknown (network error after the connection opened, or
// 5xx; for a task also a 3xx), the CLI compares our comments with this exact text in the
// history before and after: a new one means published; otherwise the outcome stays unknown
// (comment_unconfirmed, exit 1).
func (s *Service) Comment(ctx context.Context, name, ref, body string, dry bool) (any, error) {
	k, err := kindOf(name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, Usage("comment body must not be blank")
	}
	if !utf8.ValidString(body) {
		return nil, Usage("comment body must be valid UTF-8")
	}
	item, err := s.byRef(ctx, k.base, k.name, ref, 0)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/%d", k.base, ID(item["id"]))
	if dry {
		return WritePlan{true, "PATCH", path, Object{"comment": body, "version": item["version"]}}, nil
	}
	me, err := Read(ctx, s.API, "users/me", nil)
	if err != nil {
		return nil, err
	}
	before, err := s.ownComments(ctx, k, item, me, body)
	if err != nil {
		return nil, err
	}
	resp, err := confirmed(s.API.Do(ctx, taiga.Request{Method: "PATCH", Path: path, Body: map[string]any{"comment": body, "version": item["version"]}}))
	if err != nil {
		if !k.unsure(err) {
			var ae *taiga.APIError
			if errors.As(err, &ae) && ae.IsVersionConflict() {
				err = &taiga.ConflictError{Method: "PATCH", Path: path, Fields: []string{"comment"}}
			}
			return nil, taiga.ToOutput(err)
		}
		if err := s.findComment(ctx, k, item, me, body, before, path, err); err != nil {
			return nil, err
		}
		after, rerr := Read(ctx, s.API, path, nil)
		if rerr != nil {
			read := output.AsError(rerr)
			return nil, &output.Error{Code: "write_applied", Source: read.Source, Stage: "PATCH " + path,
				Cause:    fmt.Sprintf("the comment was published (PATCH %s failed with %v, but the history shows it), and the %s could not be read: %s", path, err, k.name, read.Error()),
				Recovery: fmt.Sprintf("do not re-run the command: the comment is already saved; check it with `taiga %s comments %v`", k.name, item["ref"]),
				Exit:     output.ExitUnexpected}
		}
		return s.view(k, after)
	}
	after, err := reread(ctx, s.API, "PATCH", path, path, resp)
	if err != nil {
		return nil, k.applied(err, fmt.Sprintf("`taiga %s comments %v`", k.name, item["ref"]))
	}
	return s.view(k, after)
}

// unknownOutcome is a failed write that Taiga may have applied: the request left, but no
// answer says what happened.
func unknownOutcome(err error) bool {
	var ne *taiga.NetworkError
	var ae *taiga.APIError
	return errors.As(err, &ne) || errors.As(err, &ae) && ae.Status >= 500
}

// ownComments returns the ids of the comments of me with exactly body.
func (s *Service) ownComments(ctx context.Context, k kind, item, me Object, body string) (map[string]bool, error) {
	entries, err := s.history(ctx, k, item)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, o := range entries {
		user, _ := o["user"].(map[string]any)
		if text, _ := o["comment"].(string); text == body && user != nil && ID(user["pk"]) > 0 && ID(user["pk"]) == ID(me["id"]) {
			ids[fmt.Sprint(o["id"])] = true
		}
	}
	return ids, nil
}

// findComment looks in the history for the comment of a PATCH whose outcome is unknown. Found:
// nil, the comment is published. Otherwise comment_unconfirmed, exit 1: a missing comment does
// not prove the PATCH failed, because a request still running on the server (a gateway timeout,
// a client timeout) can land after the check. A repeatable exit 7 would let scripts that retry
// network errors publish twice.
func (s *Service) findComment(ctx context.Context, k kind, item, me Object, body string, before map[string]bool, path string, sendErr error) error {
	after, err := s.ownComments(ctx, k, item, me, body)
	var checked string
	if err != nil {
		read := output.AsError(err)
		checked = "the history could not be read to check: " + read.Error()
	} else {
		for id := range after {
			if !before[id] {
				return nil
			}
		}
		checked = "the comment is not in the history yet, but the request may still be running on the server"
	}
	return &output.Error{Code: "comment_unconfirmed", Source: output.AsError(taiga.ToOutput(sendErr)).Source, Stage: "PATCH " + path,
		Cause:    fmt.Sprintf("the comment may have been published: PATCH %s failed (%v) and %s", path, sendErr, checked),
		Recovery: fmt.Sprintf("do not re-run the command blindly: wait, check with `taiga %s comments %v` and publish again only if the comment is still missing", k.name, item["ref"]),
		Exit:     output.ExitUnexpected}
}
