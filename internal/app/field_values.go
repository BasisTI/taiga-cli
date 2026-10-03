package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// ValuePath is the custom-attributes-values resource of a story or task. It has its own
// version, independent of the story's or task's.
func ValuePath(kind string, id int64) (string, error) {
	if id <= 0 {
		return "", Usage("resource id must be positive")
	}
	switch kind {
	case "story":
		return fmt.Sprintf("userstories/custom-attributes-values/%d", id), nil
	case "task":
		return fmt.Sprintf("tasks/custom-attributes-values/%d", id), nil
	}
	return "", Usage("--kind must be story or task")
}

// ParseFieldValue converts raw to the JSON value of a field of type typ. Taiga does not
// validate values (docs/api-notes.md), so the CLI does. An assignment never unsets a field:
// "null" is text, and an empty date is refused; unsetting takes UnsetFieldValue.
func ParseFieldValue(typ, raw string) (any, error) {
	switch typ {
	case "text":
		return raw, nil
	case "checkbox":
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, Usage("checkbox value must be true or false")
	case "date":
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return nil, Usage("date value must be a valid YYYY-MM-DD date: " + raw)
		}
		return raw, nil
	}
	return nil, Usage("unsupported field type: " + typ + " (text, date and checkbox can be set)")
}

// unsettable are the types whose value can be cleared (stored as null). A text field has a
// value of its own for "empty", the empty text, so unsetting it is refused.
var unsettable = map[string]bool{"checkbox": true, "date": true}

// UnsetFieldValue checks that a field of type typ can be cleared.
func UnsetFieldValue(typ, name string) error {
	if !unsettable[typ] {
		return Usage(fmt.Sprintf("field %s has type %s: only checkbox and date fields can be unset (set a text field to empty with %s=)", name, typ, name))
	}
	return nil
}

// MergeValues returns current with updates applied; keys not in updates are kept as read.
func MergeValues(current Object, updates Object) Object {
	out := Object{}
	for k, v := range current {
		out[k] = v
	}
	for k, v := range updates {
		out[k] = v
	}
	return out
}

// owners maps a kind to its resource and to the key of the values object that names it.
var owners = map[string][2]string{"story": {"userstories", "user_story"}, "task": {"tasks", "task"}}

// FieldValues reads the values of the story or task id, after checking that it belongs to the
// selected project. A read error is returned as is, never as empty values.
func (s *Service) FieldValues(ctx context.Context, kind string, id int64) (Object, error) {
	values, _, err := s.fieldValues(ctx, kind, id)
	return values, err
}

// fieldValues is FieldValues that also returns the story or task the values belong to.
func (s *Service) fieldValues(ctx context.Context, kind string, id int64) (Object, Object, error) {
	path, err := ValuePath(kind, id)
	if err != nil {
		return nil, nil, err
	}
	owner := owners[kind]
	o, err := Read(ctx, s.API, fmt.Sprintf("%s/%d", owner[0], id), nil)
	if err != nil {
		return nil, nil, err
	}
	if ID(o["project"]) != ID(s.Project["id"]) {
		return nil, nil, Usage(fmt.Sprintf("%s %d belongs to another project, not %v", kind, id, s.Project["slug"]))
	}
	values, err := Read(ctx, s.API, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if ID(values[owner[1]]) != id {
		return nil, nil, fmt.Errorf("GET %s returned the values of another %s", path, kind)
	}
	if _, ok := values["attributes_values"].(map[string]any); !ok {
		return nil, nil, fmt.Errorf("GET %s: invalid attributes_values", path)
	}
	return values, o, nil
}

// SetFieldValues merges the Name=value entries into the values of the story or task id, clears
// the fields named in unsets, and writes the whole dictionary with the version of the values
// resource. A cleared field keeps its key with null: Taiga refuses an empty dictionary, so the
// key cannot be dropped. Clearing a field without a stored value adds nothing, because an
// absent key and null both mean no value. Every entry is resolved and parsed before anything
// is read or written; an unchanged dictionary writes nothing.
func (s *Service) SetFieldValues(ctx context.Context, kind string, id int64, entries, unsets []string, dry, force bool) (any, error) {
	if len(entries)+len(unsets) == 0 {
		return nil, Usage("at least one field assignment or --unset is required")
	}
	defs, err := s.Fields(ctx, kind)
	if err != nil {
		return nil, err
	}
	updates := Object{}
	for _, entry := range entries {
		name, raw, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return nil, Usage("field assignment must be Name=value: " + entry)
		}
		def, err := Resolve(defs, name, "name")
		if err != nil {
			return nil, err
		}
		key := fmt.Sprint(def["id"])
		if _, exists := updates[key]; exists {
			return nil, Usage("duplicate field assignment: " + name)
		}
		value, err := ParseFieldValue(fmt.Sprint(def["type"]), raw)
		if err != nil {
			return nil, err
		}
		updates[key] = value
	}
	cleared := map[string]bool{}
	for _, name := range unsets {
		def, err := Resolve(defs, name, "name")
		if err != nil {
			return nil, err
		}
		key := fmt.Sprint(def["id"])
		if _, exists := updates[key]; exists || cleared[key] {
			return nil, Usage("duplicate field assignment: " + name)
		}
		if err := UnsetFieldValue(fmt.Sprint(def["type"]), name); err != nil {
			return nil, err
		}
		cleared[key] = true
	}
	before, item, err := s.fieldValues(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	current := Object(before["attributes_values"].(map[string]any))
	for key := range cleared {
		if _, stored := current[key]; stored {
			updates[key] = nil
		}
	}
	merged := MergeValues(current, updates)
	if equal(current, merged) {
		return before, nil
	}
	path, err := ValuePath(kind, id)
	if err != nil {
		return nil, err
	}
	if dry {
		return WritePlan{true, "PATCH", path, Object{"attributes_values": merged, "version": before["version"]}}, nil
	}
	k, err := kindOf(kind)
	if err != nil {
		return nil, err
	}
	return s.writeValues(ctx, k, path, before, merged, fmt.Sprintf("`taiga %s field list %v`", k.name, item["ref"]), force)
}

// writeValues sends the merged dictionary once. Taiga's OCC never refuses an old version on
// this resource (docs/api-notes.md), so the version cannot prevent a lost update: the answer is
// checked instead. It must be the next version of the read that computed the merge and hold
// exactly the dictionary sent; otherwise another write landed in between, which is reported
// as applied and never retried. --force-version skips the check. check is the command that
// shows the values, for the recovery.
func (s *Service) writeValues(ctx context.Context, k kind, path string, before, merged Object, check string, force bool) (Object, error) {
	patch := Object{"attributes_values": merged}
	resp, err := confirmed(s.API.Do(ctx, taiga.Request{Method: "PATCH", Path: path, Body: map[string]any{"attributes_values": merged, "version": before["version"]}}))
	if err != nil && k.confirmUncertain && uncertain(err) {
		return s.confirmPatch(ctx, k, path, patch, ID(before["version"])+1, err, check)
	}
	if err != nil {
		var ae *taiga.APIError
		if errors.As(err, &ae) && ae.IsVersionConflict() {
			err = &taiga.ConflictError{Method: "PATCH", Path: path, Fields: []string{"attributes_values"}}
		}
		return nil, taiga.ToOutput(err)
	}
	after, err := reread(ctx, s.API, "PATCH", path, path, resp)
	if err != nil {
		return nil, k.applied(err, check)
	}
	if force {
		return after, nil
	}
	// The PATCH answer shows what our write produced; when it cannot be read, the re-read
	// stands in (a later write then shows as a mismatch too, never as a pass).
	written, source := after, "the re-read after it has"
	if w, err := Decode(resp.Body); err == nil {
		written, source = w, "it answered"
	}
	problems := []string{}
	if ID(written["version"]) != ID(before["version"])+1 {
		problems = append(problems, fmt.Sprintf("another write landed next to this PATCH (the read before it had version %v, %s version %v)", before["version"], source, written["version"]))
	}
	if !equal(written["attributes_values"], merged) {
		problems = append(problems, fmt.Sprintf("the values differ from the request (%s %s)", source, jsonText(written["attributes_values"])))
	}
	if len(problems) > 0 {
		return nil, &output.Error{Code: "field_values_postcondition_failed", Source: "api", Stage: "PATCH " + path,
			Cause: fmt.Sprintf("the change was applied (PATCH %s returned HTTP %d), but %s; now attributes_values=%s (version %v)",
				path, resp.Status, strings.Join(problems, "; "), jsonText(after["attributes_values"]), after["version"]),
			Recovery: "do not re-run the command blindly: someone else wrote the custom fields at the same time and Taiga does not detect it; check them with " + check + " and set what is missing",
			Exit:     output.ExitConflict}
	}
	return after, nil
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// FieldsView binds the values to their definitions: each definition gets the stored value
// under "value" when there is one. Ids without a definition stay in attributes_values.
func FieldsView(values Object, defs []Object) []Object {
	stored, _ := values["attributes_values"].(map[string]any)
	out := []Object{}
	for _, def := range defs {
		field := Object{}
		for k, v := range def {
			field[k] = v
		}
		if v, ok := stored[fmt.Sprint(def["id"])]; ok {
			field["value"] = v
		}
		out = append(out, field)
	}
	return out
}
