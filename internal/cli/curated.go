package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/spf13/cobra"
)

// service builds the per-run application service; credentials come only from App.client.
func (a *App) service(cmd *cobra.Command) (*app.Service, error) {
	rc, err := a.runContext()
	if err != nil {
		return nil, err
	}
	c, err := a.client(cmd.Context(), rc)
	if err != nil {
		return nil, err
	}
	return app.New(cmd.Context(), c, rc.Ctx.Project.Value)
}

// readContent returns the exact bytes of FILE, or of stdin for "-".
func (a *App) readContent(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(a.In)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", app.Usage("cannot read input file: " + err.Error())
	}
	return string(b), nil
}

// storyTextKeys is the text summary of a story; JSON output carries the whole object.
var storyTextKeys = []string{"ref", "id", "version", "subject", "status", "is_closed", "tags", "assigned_to", "assigned_users", "is_blocked", "url"}

func (a *App) renderCurated(v any) error { return a.renderKeys(v, storyTextKeys) }

// renderKeys writes v as JSON, or in text as the given keys of each object, redacted on the
// way out (presentable): signed media URLs keep their key, not their token.
func (a *App) renderKeys(v any, textKeys []string) error {
	v, err := presentable(v)
	if err != nil {
		return err
	}
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return a.writeJSON(v)
	}
	switch x := v.(type) {
	case app.Object:
		fields := []output.Field{}
		for _, k := range textKeys {
			val, ok := x[k]
			if !ok {
				continue
			}
			if val == nil {
				val = ""
			}
			if k == "status" {
				if info, ok := x["status_extra_info"].(map[string]any); ok && info["name"] != nil {
					val = fmt.Sprintf("%v (%v)", info["name"], val)
				}
			}
			if names, ok := val.([]string); ok {
				val = strings.Join(names, ", ")
			}
			if links, ok := val.([]app.Object); ok {
				val = strings.Join(linkedStoryRefs(links), ", ")
			}
			text := fmt.Sprint(val)
			if strings.ContainsFunc(text, unsafeRune) {
				text = strconv.Quote(text) // one line per key: a line break cannot forge another key
			}
			fields = append(fields, output.Field{Key: k, Value: text})
		}
		return a.writeFields(fields)
	case []app.Object:
		for i, o := range x {
			if i > 0 {
				if err := a.printLine(""); err != nil {
					return err
				}
			}
			if err := a.renderKeys(o, textKeys); err != nil {
				return err
			}
		}
		return nil
	default:
		// Dry-run plans are nested: text mode prints each top-level key with its JSON value.
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(b, &object); err != nil {
			return err
		}
		keys := []string{}
		for k := range object {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := []output.Field{}
		for _, k := range keys {
			fields = append(fields, output.Field{Key: k, Value: escapeJSON(string(object[k]))})
		}
		return a.writeFields(fields)
	}
}

// linkedStoryRefs is the text form of linked stories (epic get): the ref, prefixed with the project
// slug for a story of another project, or the id when the account cannot read the story.
func linkedStoryRefs(links []app.Object) []string {
	out := []string{}
	for _, l := range links {
		switch {
		case l["ref"] == nil:
			out = append(out, fmt.Sprintf("id:%v", l["id"]))
		case l["project_slug"] != nil:
			out = append(out, fmt.Sprintf("%v#%v", l["project_slug"], l["ref"]))
		default:
			out = append(out, fmt.Sprint(l["ref"]))
		}
	}
	return out
}

// searchFlag adds --search to cmd; the returned func checks it was not given blank.
func searchFlag(cmd *cobra.Command, search *string, what string) func() error {
	cmd.Flags().StringVar(search, "search", "", what+" contains this text, ignoring case (accents must match)")
	return func() error {
		if cmd.Flags().Changed("search") && strings.TrimSpace(*search) == "" {
			return app.Usage("--search must not be blank")
		}
		return nil
	}
}

// closedFlag adds --closed to cmd; the returned func gives nil when it was not given.
func closedFlag(cmd *cobra.Command, closed *bool, what string) func() *bool {
	cmd.Flags().BoolVar(closed, "closed", false, "only closed "+what+" (--closed=false: only open ones)")
	return func() *bool {
		if !cmd.Flags().Changed("closed") {
			return nil
		}
		return closed
	}
}

// escapeJSON writes each unsafe rune of JSON text as a \u escape. Such runes only occur inside
// JSON strings, where the escape keeps the text valid and equal once decoded.
func escapeJSON(s string) string {
	if !strings.ContainsFunc(s, unsafeRune) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if !unsafeRune(r) {
			b.WriteRune(r)
			continue
		}
		if r > 0xffff {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&b, "\\u%04x\\u%04x", r1, r2)
			continue
		}
		fmt.Fprintf(&b, "\\u%04x", r)
	}
	return b.String()
}

// unsafeRune is a rune that can break or disguise a "key: value" line (output.UnsafeRune).
func unsafeRune(r rune) bool { return output.UnsafeRune(r) }
