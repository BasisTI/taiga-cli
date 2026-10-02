package cli

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// Every curated command prints through writeJSON or writeFields, which hide the value of
// every token parameter in what they print (taiga.RedactTokens): user photos, project logos,
// attachment links and anything a description or a custom field holds are signed media URLs
// that open without authentication. The rule is on the way out, so no type can skip it:
// objects, write plans, upload plans and results alike. It changes only the printout; the
// requests keep their values. `taiga api` alone prints Taiga's answer raw (api.go), and
// TestOnlyAPIPrintsRaw keeps it that way.

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

// writeJSON prints v as indented JSON, redacted.
func (a *App) writeJSON(v any) error {
	p, err := presentable(v)
	if err != nil {
		return err
	}
	return output.WriteJSON(a.Out, p)
}

// printLine prints one line of text, redacted and on one line.
func (a *App) printLine(s string) error {
	_, err := io.WriteString(a.Out, output.Quote(taiga.RedactTokens(s))+"\n")
	return err
}

// writeFields prints "key: value" lines, redacted and one line per key.
func (a *App) writeFields(fields []output.Field) error {
	out := make([]output.Field, len(fields))
	for i, f := range fields {
		out[i] = output.Field{Key: output.Quote(taiga.RedactTokens(f.Key)), Value: output.Quote(taiga.RedactTokens(f.Value))}
	}
	return output.WriteFields(a.Out, out)
}
