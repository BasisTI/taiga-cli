package app

import (
	"context"
	"fmt"
	"net/url"
)

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
