package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

// present.go is the presentation layer and the only code that writes to the process: stdout
// (App.Out) and stderr (App.Err). TestOutputGoesThroughPresent checks it on the syntax tree;
// only cmd/taiga/main.go names os.Stdout and os.Stderr.
//
// Redaction happens once, on content, before anything is escaped or serialized: every string
// of what is printed goes through taiga.RedactTokens (app.Scrub, presentable), so a signed
// media URL (user photo, project logo, attachment link) keeps its key and loses its token
// value. Redacting content rather than the final text keeps JSON valid when a whole string
// must go, sees keys before JSON escapes (\u0074oken) could hide them, and never rescans
// escaped text, where "\n" is not a delimiter and a second pass would swallow what follows.
//
//   - writeJSON, writeFields, printLine, renderKeys, writeError, warn and prompt take raw
//     content and redact it once;
//   - emitJSON, emitFields, emitOut and emitErr write content that is already presented;
//   - rawOut is stdout without presentation, for bytes that are not CLI text: the answers of
//     `taiga api` and a downloaded file with `--to -`;
//   - Cobra gets line writers that redact its own text (help), which nothing else redacted.
//
// Only the printout changes: requests keep their values.

// rawOut is stdout as is. Only `taiga api` and `attachment download --to -` use it.
func (a *App) rawOut() io.Writer { return a.Out }

// wireOutput gives Cobra writers that redact each line of its own text; flush writes what is
// left after the command ends.
func (a *App) wireOutput(root *cobra.Command) (flush func()) {
	out, errw := &lineRedactor{w: a.Out}, &lineRedactor{w: a.Err}
	root.SetOut(out)
	root.SetErr(errw)
	return func() { out.flush(); errw.flush() }
}

// lineRedactor redacts whole lines of text written to it, once each.
type lineRedactor struct {
	w   io.Writer
	buf []byte
}

func (l *lineRedactor) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if _, err := io.WriteString(l.w, taiga.RedactTokens(string(l.buf[:i]))+"\n"); err != nil {
			return 0, err
		}
		l.buf = l.buf[i+1:]
	}
}

func (l *lineRedactor) flush() {
	if len(l.buf) > 0 {
		_, _ = io.WriteString(l.w, taiga.RedactTokens(string(l.buf)))
		l.buf = nil
	}
}

// emitJSON, emitFields, emitOut and emitErr write presented content, without redacting again.
func (a *App) emitJSON(v any) error { return output.WriteJSON(a.Out, v) }

func (a *App) emitFields(fields []output.Field) error { return output.WriteFields(a.Out, fields) }

func (a *App) emitOut(s string) error {
	_, err := io.WriteString(a.Out, s)
	return err
}

func (a *App) emitErr(s string) error {
	_, err := io.WriteString(a.Err, s)
	return err
}

// presented is s redacted, then quoted when it has a control or format character, so it
// stays on one line.
func presented(s string) string { return output.Quote(taiga.RedactTokens(s)) }

// writeJSON prints v as indented JSON, redacted.
func (a *App) writeJSON(v any) error {
	p, err := presentable(v)
	if err != nil {
		return err
	}
	return a.emitJSON(p)
}

// writeFields prints "key: value" lines from raw text, redacted once and one line per key.
func (a *App) writeFields(fields []output.Field) error {
	out := make([]output.Field, len(fields))
	for i, f := range fields {
		out[i] = output.Field{Key: presented(f.Key), Value: presented(f.Value)}
	}
	return a.emitFields(out)
}

// printLine prints one line of raw text, redacted and on one line.
func (a *App) printLine(s string) error { return a.emitOut(presented(s) + "\n") }

// writeError prints e to stderr with every text field redacted once; output.WriteError then
// quotes what has control or format characters.
func (a *App) writeError(mode output.Mode, e *output.Error) error {
	r := *e
	r.Code, r.Source, r.Stage = taiga.RedactTokens(r.Code), taiga.RedactTokens(r.Source), taiga.RedactTokens(r.Stage)
	r.Cause, r.Recovery = taiga.RedactTokens(r.Cause), taiga.RedactTokens(r.Recovery)
	return output.WriteError(a.Err, mode, &r)
}

// warn prints a warning to stderr, redacted.
func (a *App) warn(code, msg string) {
	_ = a.emitErr("warning [" + presented(code) + "]: " + presented(msg) + "\n")
}

// prompt asks for input on stderr, so stdout stays clean for the result. The text is redacted
// and kept on one line; the line breaks that end it are written as they are.
func (a *App) prompt(s string) {
	text := strings.TrimRight(s, "\n")
	if text != "" {
		text = presented(text)
	}
	_ = a.emitErr(text + s[len(strings.TrimRight(s, "\n")):])
}

// presentable returns v as it may be printed. Objects keep their type, for the text layout;
// any other value is rewritten through its JSON, with the same key order and numbers.
func presentable(v any) (any, error) {
	switch x := v.(type) {
	case app.Object, []app.Object:
		return app.Scrub(x), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var out bytes.Buffer
	if err := redactJSON(d, &out); err != nil {
		return nil, err
	}
	return json.RawMessage(out.Bytes()), nil
}

// redactJSON copies one JSON value from d to out, with every string (keys included) redacted.
func redactJSON(d *json.Decoder, out *bytes.Buffer) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch x := t.(type) {
	case json.Delim:
		out.WriteRune(rune(x))
		for i := 0; d.More(); i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			if x == '{' {
				k, err := d.Token()
				if err != nil {
					return err
				}
				writeString(out, k.(string))
				out.WriteByte(':')
			}
			if err := redactJSON(d, out); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil {
			return err
		}
		out.WriteRune(rune(end.(json.Delim)))
	case string:
		writeString(out, x)
	case json.Number:
		out.WriteString(x.String())
	case bool:
		if x {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case nil:
		out.WriteString("null")
	}
	return nil
}

func writeString(out *bytes.Buffer, s string) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(taiga.RedactTokens(s))
	out.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// renderKeys writes v as JSON, or in text as the given keys of each object. v is redacted once,
// as content (presentable): signed media URLs keep their key, not their token.
func (a *App) renderKeys(v any, textKeys []string) error {
	p, err := presentable(v)
	if err != nil {
		return err
	}
	return a.renderPresented(p, textKeys)
}

// renderPresented lays out v, which presentable already redacted: nothing here redacts again,
// so escaping (a quoted line break, JSON text) never meets the scanner.
func (a *App) renderPresented(v any, textKeys []string) error {
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return a.emitJSON(v)
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
		return a.emitFields(fields)
	case []app.Object:
		for i, o := range x {
			if i > 0 {
				if err := a.emitOut("\n"); err != nil {
					return err
				}
			}
			if err := a.renderPresented(o, textKeys); err != nil {
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
		return a.emitFields(fields)
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

// renderStoryFields prints the values of a story next to their definitions. version is the
// version of the values resource, not the story's.
func (a *App) renderStoryFields(ctx context.Context, service *app.Service, story, values app.Object) error {
	defs, err := service.Fields(ctx, "story")
	if err != nil {
		return err
	}
	view, err := service.StoryView(story)
	if err != nil {
		return err
	}
	// Redacted once, as content; the lines below only lay it out.
	result := app.Scrub(app.Object{"id": story["id"], "ref": story["ref"], "url": view["url"], "version": values["version"],
		"attributes_values": values["attributes_values"], "fields": app.FieldsView(values, defs)}).(app.Object)
	fields, _ := result["fields"].([]app.Object)
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return a.emitJSON(result)
	}
	lines := []output.Field{{Key: "ref", Value: fmt.Sprint(result["ref"])}, {Key: "id", Value: fmt.Sprint(result["id"])},
		{Key: "version", Value: fmt.Sprint(result["version"])}, {Key: "url", Value: jsonValue(result["url"])}}
	known := map[string]bool{}
	for _, f := range fields {
		known[fmt.Sprint(f["id"])] = true
		value := ""
		if v, ok := f["value"]; ok {
			value = jsonValue(v)
		}
		lines = append(lines, output.Field{Key: fmt.Sprintf("%s (%s)", jsonValue(f["name"]), jsonValue(f["type"])), Value: value})
	}
	stored, _ := result["attributes_values"].(map[string]any)
	for _, id := range sortedKeys(stored) {
		if !known[id] {
			lines = append(lines, output.Field{Key: "#" + id + " (no definition)", Value: jsonValue(stored[id])})
		}
	}
	return a.emitFields(lines)
}

// jsonValue prints plain text as is; empty text, text with control or format characters (line
// breaks and bidi overrides included) and any other value print as JSON, so each field stays on one line and a stored ""
// differs from a field without value. A cleared value (null) prints empty, like a field without
// value, so it never reads as the text "null"; JSON output keeps the null.
func jsonValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok && s != "" && !strings.ContainsFunc(s, unsafeRune) {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return escapeJSON(string(b))
}
