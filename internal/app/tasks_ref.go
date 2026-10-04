package app

import (
	"context"
	"fmt"
	"net/url"
)

// kind is what stories and tasks differ in, for the code they share.
type kind struct {
	name     string // story or task: command and message names
	base     string // the API resource
	web      string // the path segment of the web URL
	statuses string // the status catalog
	history  string // the history resource
	// fenceAssignees: Taiga's OCC never sees a story's assigned_to (docs/api-notes.md), so a
	// story write that changes assignees is fenced (writeAssignees); a task's assigned_to is in
	// its history diff and the per-field OCC protects it.
	fenceAssignees bool
}

var (
	storyKind = kind{name: "story", base: "userstories", web: "us", statuses: "userstory-statuses", history: "userstory", fenceAssignees: true}
	taskKind  = kind{name: "task", base: "tasks", web: "task", statuses: "task-statuses", history: "task"}
)

func kindOf(name string) (kind, error) {
	switch name {
	case "story":
		return storyKind, nil
	case "task":
		return taskKind, nil
	}
	return kind{}, Usage("kind must be story or task")
}

// Item reads a story or task (name) of the selected project by ref.
func (s *Service) Item(ctx context.Context, name, ref string) (Object, error) {
	k, err := kindOf(name)
	if err != nil {
		return nil, err
	}
	return s.byRef(ctx, k.base, k.name, ref, 0)
}

// View is the output form of a story or task (name).
func (s *Service) View(name string, o Object) (Object, error) {
	k, err := kindOf(name)
	if err != nil {
		return nil, err
	}
	return s.view(k, o)
}

// Task reads a task of the selected project by ref (tasks/by_ref) or by id.
func (s *Service) Task(ctx context.Context, ref string, id int64) (Object, error) {
	return s.byRef(ctx, "tasks", "task", ref, id)
}

// byRef reads a story or task (base "userstories" or "tasks") by ref or by id and checks that
// it is the one asked for, in the selected project: a ref is never an id.
func (s *Service) byRef(ctx context.Context, base, label, ref string, id int64) (Object, error) {
	if (ref == "") == (id == 0) {
		return nil, Usage("choose REF or --id")
	}
	path, q := base+"/by_ref", url.Values{}
	var expected int64
	if ref != "" {
		var err error
		expected, err = positive(ref)
		if err != nil {
			return nil, err
		}
		q.Set("project", s.projectID())
		q.Set("ref", fmt.Sprint(expected))
	} else {
		if id <= 0 {
			return nil, Usage("id must be positive")
		}
		path, q = fmt.Sprintf("%s/%d", base, id), nil
	}
	o, err := Read(ctx, s.API, path, q)
	if err != nil {
		return nil, err
	}
	if ID(o["project"]) != ID(s.Project["id"]) {
		return nil, Usage(fmt.Sprintf("%s belongs to another project, not %v", label, s.Project["slug"]))
	}
	if ID(o["id"]) <= 0 || ID(o["ref"]) <= 0 {
		return nil, fmt.Errorf("invalid %s identity", label)
	}
	if expected > 0 && ID(o["ref"]) != expected {
		return nil, fmt.Errorf("by_ref returned another reference")
	}
	if id > 0 && ID(o["id"]) != id {
		return nil, fmt.Errorf("GET returned another id")
	}
	return o, nil
}
