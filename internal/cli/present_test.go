package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Curated output is redacted on the way out (present.go). Only `taiga api` prints raw, so no
// other file may write JSON or fields to stdout around writeJSON and writeFields.
func TestOnlyAPIPrintsRaw(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "api.go" || f == "present.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{"output.WriteJSON(", "output.WriteFields(", "a.Out.Write(", "fmt.Fprint(a.Out", "fmt.Fprintf(a.Out", "fmt.Fprintln(a.Out", "io.WriteString(a.Out"} {
			if strings.Contains(string(b), raw) {
				t.Errorf("%s prints with %s: use a.writeJSON or a.writeFields", f, raw)
			}
		}
	}
}

// A plan is printed with its key order and numbers, and only token values change.
func TestPresentableKeepsPlanShape(t *testing.T) {
	type plan struct {
		DryRun bool           `json:"dry_run"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	got, err := presentable(plan{true, "x?token=SECRET", map[string]any{"n": 12345678901234567, "s": "<a href=\"h?token=S\">"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := got.([]byte)
	if raw, ok := got.(interface{ MarshalJSON() ([]byte, error) }); ok {
		b, _ = raw.MarshalJSON()
	}
	if string(b) != `{"dry_run":true,"path":"x?token=…","body":{"n":12345678901234567,"s":"<a href=\"h?token=…\">"}}` {
		t.Fatalf("%s", b)
	}
}
