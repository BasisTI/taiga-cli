package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
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

// renderKeys writes v as JSON, or in text as the given keys of each object.
func (a *App) renderKeys(v any, textKeys []string) error {
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return output.WriteJSON(a.Out, v)
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
			text := fmt.Sprint(val)
			if strings.ContainsFunc(text, unsafeRune) {
				text = strconv.Quote(text) // one line per key: a line break cannot forge another key
			}
			fields = append(fields, output.Field{Key: k, Value: text})
		}
		return output.WriteFields(a.Out, fields)
	case []app.Object:
		for i, o := range x {
			if i > 0 {
				if _, err := fmt.Fprintln(a.Out); err != nil {
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
		return output.WriteFields(a.Out, fields)
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

// unsafeRune is a rune that can break or disguise a "key: value" line: controls, line and
// paragraph separators, and format characters such as bidi overrides.
func unsafeRune(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}
