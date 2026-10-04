package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// FieldPath is the definitions endpoint of a kind of resource.
func FieldPath(kind string) (string, error) {
	switch kind {
	case "story":
		return "userstory-custom-attributes", nil
	case "task":
		return "task-custom-attributes", nil
	}
	return "", Usage("--kind must be story or task")
}

// fieldTypes are the types validated against the local Taiga (docs/api-notes.md). Taiga also
// has multiline, richtext, url, dropdown and number; they wait for their own validation.
var fieldTypes = map[string]bool{"text": true, "date": true, "checkbox": true}

// ValidateField checks a definition before any request. Taiga limits names to 64 characters;
// "=" is refused because Name=value assignments end the name at the first "=".
func ValidateField(name, typ string) error {
	if strings.TrimSpace(name) == "" {
		return Usage("field name is required")
	}
	if strings.Contains(name, "=") {
		return Usage("field name cannot contain \"=\": story field set could not address it by name")
	}
	if utf8.RuneCountInString(name) > 64 {
		return Usage("field name is longer than 64 characters")
	}
	if !fieldTypes[typ] {
		return Usage("unsupported field type: " + typ + " (use text, date or checkbox)")
	}
	return nil
}

// Fields lists the custom field definitions of the project for kind.
func (s *Service) Fields(ctx context.Context, kind string) ([]Object, error) {
	path, err := FieldPath(kind)
	if err != nil {
		return nil, err
	}
	return s.Catalog(ctx, path)
}

// CreateField creates a definition unless one with the same name (case-sensitive, as Taiga
// compares) already exists: a compatible one is returned unchanged, a different one is an
// error. Existing definitions are never modified. A nil description is not compared.
func (s *Service) CreateField(ctx context.Context, kind, name, typ string, description *string, dry bool) (any, error) {
	if err := ValidateField(name, typ); err != nil {
		return nil, err
	}
	path, err := FieldPath(kind)
	if err != nil {
		return nil, err
	}
	existing, err := s.existingField(ctx, kind, name, typ, description)
	if existing != nil || err != nil {
		return existing, err
	}
	body := Object{"project": s.Project["id"], "name": name, "type": typ}
	if description != nil {
		body["description"] = *description
	}
	if dry {
		return WritePlan{true, "POST", path, body}, nil
	}
	r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: path, Body: body})
	delete(s.catalogs, path)
	if err != nil {
		// Taiga refuses a second definition with the same name (400 on "name"). The POST did
		// not apply, so reading again is safe: another run may have just created it.
		var ae *taiga.APIError
		if errors.As(err, &ae) && ae.Status == 400 && hasKey(ae.Body, "name") {
			if existing, rerr := s.existingField(ctx, kind, name, typ, description); existing != nil || rerr != nil {
				return existing, rerr
			}
		}
		if uncertain(err) {
			return nil, s.fieldUnconfirmed(ctx, kind, name, path, err)
		}
		return nil, taiga.ToOutput(err)
	}
	created, err := Decode(r.Body)
	if err == nil && ID(created["id"]) <= 0 {
		err = fmt.Errorf("the response has no field id")
	}
	if err != nil {
		return nil, WriteApplied("POST", path, r.Status, taiga.ToOutput(fmt.Errorf("decode the created field: %w", err)))
	}
	return reread(ctx, s.API, "POST", path, fmt.Sprintf("%s/%d", path, ID(created["id"])), r)
}

// fieldUnconfirmed is field_create_unconfirmed, exit 1, for a POST whose outcome is unknown
// (network after the connection opened, 5xx, 3xx). A definition with the name found afterwards
// is named, never adopted: nothing proves this POST created it (decision of 2026-10-03, option
// B), and none found proves no absence, because the POST may still be running on the server.
// Taiga refuses a second definition with the same name, so a later run can never duplicate it.
func (s *Service) fieldUnconfirmed(ctx context.Context, kind, name, path string, sendErr error) error {
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	delete(s.catalogs, path)
	fields, err := s.Fields(check, kind)
	checked := "the project has no field with this name yet, but the request may still be running on the server"
	if err != nil {
		checked = "the fields could not be read to look for it: " + output.AsError(err).Error()
	} else {
		found := []string{}
		for _, f := range fields {
			if f["name"] == name {
				found = append(found, fmt.Sprintf("id %d, type %v", ID(f["id"]), f["type"]))
			}
		}
		if len(found) > 0 {
			checked = fmt.Sprintf("the project has a field with this name (%s), but nothing proves this command created it", strings.Join(found, "; "))
		}
	}
	return &output.Error{Code: "field_create_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "POST " + path,
		Cause:    fmt.Sprintf("the field %q may have been created: POST %s failed (%v) and %s", name, path, sendErr, checked),
		Recovery: fmt.Sprintf("do not assume it is missing: wait and check with `taiga field list --kind %s`; Taiga refuses a second field with the same name, so it is never created twice", kind),
		Exit:     output.ExitUnexpected}
}

// existingField returns the definition named name, nil when there is none, or an error when
// it is ambiguous or differs from the request.
func (s *Service) existingField(ctx context.Context, kind, name, typ string, description *string) (Object, error) {
	fields, err := s.Fields(ctx, kind)
	if err != nil {
		return nil, err
	}
	matches := []Object{}
	for _, field := range fields {
		if field["name"] == name {
			matches = append(matches, field)
		}
	}
	switch {
	case len(matches) > 1:
		return nil, &output.Error{Code: "ambiguous_name", Cause: "duplicate field name: " + name, Recovery: "fix the duplicate definitions in Taiga", Exit: output.ExitUsage}
	case len(matches) == 0:
		return nil, nil
	}
	field := matches[0]
	if field["type"] != typ || (description != nil && fmt.Sprint(field["description"]) != *description) {
		return nil, &output.Error{Code: "field_definition_conflict",
			Cause:    fmt.Sprintf("field %q already exists with type %v and description %q", name, field["type"], fmt.Sprint(field["description"])),
			Recovery: "review the existing definition; no changes were made", Exit: output.ExitUsage}
	}
	return field, nil
}

func hasKey(body []byte, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
