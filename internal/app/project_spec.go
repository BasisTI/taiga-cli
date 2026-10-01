package app

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
)

// StatusSpec declares a story status. Closed defaults to false; After names the status it
// follows (existing or declared in the same file).
type StatusSpec struct {
	Name   string `toml:"name"`
	Color  string `toml:"color"`
	Closed bool   `toml:"closed"`
	After  string `toml:"after"`
}

// FieldSpec declares a story custom field; an absent description means "".
type FieldSpec struct {
	Name        string `toml:"name"`
	Type        string `toml:"type"`
	Description string `toml:"description"`
}

// ProjectSpec is the project TOML: only what it declares is managed, nothing is removed.
type ProjectSpec struct {
	StoryStatus []StatusSpec `toml:"story_status"`
	StoryField  []FieldSpec  `toml:"story_field"`
}

var statusColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ParseProjectSpec decodes the project TOML strictly (unknown keys are errors) and validates
// every declaration before anything is read from Taiga.
func ParseProjectSpec(r io.Reader) (ProjectSpec, error) {
	var spec ProjectSpec
	d := toml.NewDecoder(r).DisallowUnknownFields()
	if err := d.Decode(&spec); err != nil {
		return spec, Usage("invalid project TOML: " + err.Error())
	}
	if err := specDuplicates(spec); err != nil {
		return spec, err
	}
	seen := map[string]bool{}
	for _, st := range spec.StoryStatus {
		switch {
		case strings.TrimSpace(st.Name) == "":
			return spec, Usage("status name is required")
		case utf8.RuneCountInString(st.Name) > 255:
			return spec, Usage("status name is longer than 255 characters: " + st.Name)
		case !statusColor.MatchString(st.Color):
			return spec, Usage(fmt.Sprintf("status %q: color must be #RRGGBB", st.Name))
		case st.Name == st.After:
			return spec, Usage(fmt.Sprintf("status %q cannot come after itself", st.Name))
		case seen[st.Name]:
			return spec, Usage("duplicate status: " + st.Name)
		}
		seen[st.Name] = true
	}
	seen = map[string]bool{}
	for _, f := range spec.StoryField {
		if err := ValidateField(f.Name, f.Type); err != nil {
			return spec, err
		}
		if seen[f.Name] {
			return spec, Usage("duplicate field: " + f.Name)
		}
		seen[f.Name] = true
	}
	return spec, nil
}

// specDuplicates refuses two declarations whose names differ only by case: Taiga would accept
// both, and the plan treats a case variant of an existing name as drift.
func specDuplicates(spec ProjectSpec) error {
	statuses, fields := []string{}, []string{}
	for _, st := range spec.StoryStatus {
		statuses = append(statuses, st.Name)
	}
	for _, f := range spec.StoryField {
		fields = append(fields, f.Name)
	}
	for _, c := range []struct {
		kind  string
		names []string
	}{{"status", statuses}, {"field", fields}} {
		kind, names := c.kind, c.names
		for i, a := range names {
			for _, b := range names[i+1:] {
				if strings.EqualFold(a, b) {
					return Usage(fmt.Sprintf("duplicate %s: %q and %q differ only by case", kind, a, b))
				}
			}
		}
	}
	return nil
}

// OrderStatuses returns the desired order of every status: current ones (in their order) plus
// the declared ones that do not exist yet, appended in file order, each declaration with
// After moved right behind its predecessor. Statuses after the same predecessor keep the file
// order. An unknown predecessor or a cycle is an error.
func OrderStatuses(current []string, specs []StatusSpec) ([]string, error) {
	order := append([]string(nil), current...)
	exists, declared := map[string]bool{}, map[string]bool{}
	for _, name := range order {
		if exists[name] {
			return nil, Usage("duplicate remote status: " + name)
		}
		exists[name] = true
	}
	for _, st := range specs {
		if declared[st.Name] {
			return nil, Usage("duplicate status: " + st.Name)
		}
		declared[st.Name] = true
		if !exists[st.Name] {
			order = append(order, st.Name)
			exists[st.Name] = true
		}
	}
	after := map[string]string{}
	for _, st := range specs {
		if st.After == "" {
			continue
		}
		if st.After == st.Name {
			return nil, Usage(fmt.Sprintf("status %q cannot come after itself", st.Name))
		}
		if !exists[st.After] {
			return nil, Usage(fmt.Sprintf("status %q comes after unknown status %q", st.Name, st.After))
		}
		after[st.Name] = st.After
	}
	mark := map[string]int{}
	var check func(string) error
	check = func(name string) error {
		switch mark[name] {
		case 1:
			return Usage("cycle in status order: " + name)
		case 2:
			return nil
		}
		mark[name] = 1
		if prev := after[name]; prev != "" {
			if err := check(prev); err != nil {
				return err
			}
		}
		mark[name] = 2
		return nil
	}
	for _, st := range specs { // file order: the error names the same status on every run
		if err := check(st.Name); err != nil {
			return nil, err
		}
	}
	// Roots keep their place; each status is followed by the ones declared after it.
	children := map[string][]string{}
	for _, st := range specs {
		if st.After != "" {
			children[st.After] = append(children[st.After], st.Name)
		}
	}
	out := []string{}
	var emit func(string)
	emit = func(name string) {
		out = append(out, name)
		for _, child := range children[name] {
			emit(child)
		}
	}
	for _, name := range order {
		if after[name] == "" {
			emit(name)
		}
	}
	if len(out) != len(order) {
		return nil, fmt.Errorf("invalid status ordering")
	}
	return out, nil
}
