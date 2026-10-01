package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const values6808 = "userstories/custom-attributes-values/6808"

func TestStoryFieldListGolden(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "list", "246")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "story_fields.json", strings.ReplaceAll(out, f.srv, "http://taiga.test"))
	text, _, code := runIn(t, f.env(), "", "story", "field", "list", "246", "--output", "text")
	if code != 0 {
		t.Fatal(code)
	}
	golden(t, "story_fields.txt", strings.ReplaceAll(text, f.srv, "http://taiga.test"))
	if len(writes(calls)) != 0 {
		t.Fatalf("list wrote: %+v", writes(calls))
	}
}

func TestStoryFieldSetMergesWithTheValuesVersion(t *testing.T) {
	f, calls := fieldFake(t)
	text := "a=b \"aspas\"\n\tç \u0001"
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas="+text, "Data de entrega=2026-09-30", "Testado em staging=false")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || w[0].method != "PATCH" || w[0].path != "/api/v1/"+values6808 {
		t.Fatalf("%+v", w)
	}
	if w[0].body["version"] != float64(19) {
		t.Fatalf("version of the story sent instead of the values': %v", w[0].body["version"])
	}
	want := map[string]any{"27": false, "28": "2026-09-30", "29": text, "999": "orphan"}
	if fmt.Sprint(w[0].body["attributes_values"]) != fmt.Sprint(want) || len(w[0].body) != 2 {
		t.Fatalf("body %v", w[0].body)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view["version"] != float64(20) || view["ref"] != float64(246) || view["id"] != float64(6808) || fmt.Sprint(view["attributes_values"]) != fmt.Sprint(want) {
		t.Fatalf("%s", out)
	}
	if f.stories[6808]["version"] != 7 {
		t.Fatal("the story version moved")
	}
	for _, c := range w {
		if strings.HasPrefix(c.path, "/api/v1/userstories/6808") {
			t.Fatal("custom fields sent in the story PATCH")
		}
	}
}

func TestStoryFieldSetUnchangedWritesNothing(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Testado em staging=true")
	if code != 0 || len(writes(calls)) != 0 || !strings.Contains(out, `"version": 19`) {
		t.Fatalf("%d %s %s", code, stderr, out)
	}
}

func TestStoryFieldSetDryRunGolden(t *testing.T) {
	f, calls := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "story_fields_dry_run.json", out)
}

func TestStoryFieldSetRejectsBadAssignmentsWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"Inexistente=1"}, 5, "not_found"},
		{[]string{"Notas=a", "Notas=b"}, 2, "duplicate field assignment"},
		{[]string{"Notas=a", "29=b"}, 2, "duplicate field assignment"},
		{[]string{"Testado em staging=yes"}, 2, "checkbox"},
		{[]string{"Data de entrega=30/09/2026"}, 2, "YYYY-MM-DD"},
		{[]string{"Data de entrega="}, 2, "YYYY-MM-DD"},
		{[]string{"Notas"}, 2, "Name=value"},
		{[]string{"=x"}, 2, "Name=value"},
		{[]string{"Notas=ok", "Inexistente=1"}, 5, "not_found"},
	} {
		f, calls := fieldFake(t)
		_, stderr, code := runIn(t, f.env(), "", append([]string{"story", "field", "set", "246"}, tc.args...)...)
		if code != tc.code || !strings.Contains(stderr, tc.want) {
			t.Fatalf("%v: %d %s", tc.args, code, stderr)
		}
		if len(writes(calls)) != 0 {
			t.Fatalf("%v: wrote", tc.args)
		}
	}
}

func TestStoryFieldAmbiguousDefinitionIsRefused(t *testing.T) {
	f, calls := fieldFake(t)
	f.defs["userstory-custom-attributes"] = append(f.defs["userstory-custom-attributes"],
		map[string]any{"id": 40, "name": "Notas", "type": "text", "project": 37})
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 2 || !strings.Contains(stderr, "ambiguous_name") || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
}

// Taiga accepts an old version on the values resource, so a write between our read and our
// PATCH is lost without an error. The answer's version gives it away: exit 4, applied.
func TestStoryFieldSetDetectsAConcurrentWrite(t *testing.T) {
	f, calls := fieldFake(t)
	f.onValues = func(method string, v map[string]any) {
		if method == "PATCH" {
			v["attributes_values"] = map[string]any{"27": true, "999": "orphan", "28": "2027-01-01"}
			v["version"] = 20
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 4 || !strings.Contains(stderr, "field_values_postcondition_failed") || !strings.Contains(stderr, "the change was applied") ||
		!strings.Contains(stderr, "version 19") {
		t.Fatalf("%d %s", code, stderr)
	}
	if len(writes(calls)) != 1 {
		t.Fatalf("retried: %+v", writes(calls))
	}

	f, _ = fieldFake(t)
	f.onValues = func(method string, v map[string]any) {
		if method == "PATCH" {
			v["version"] = 20
		}
	}
	if _, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x", "--force-version"); code != 0 {
		t.Fatalf("--force-version: %d %s", code, stderr)
	}
}

func TestStoryFieldSetVersionRefusalIsAConflict(t *testing.T) {
	f, calls := fieldFake(t)
	f.onValues = func(method string, v map[string]any) {
		if method == "PATCH" {
			v["version"] = 3
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 4 || !strings.Contains(stderr, "version_conflict") || !strings.Contains(stderr, "attributes_values") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryFieldSetUnreadableAnswer(t *testing.T) {
	// The re-read stands in for the answer and is checked the same way.
	f, _ := fieldFake(t)
	f.badValuesWrite = true
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 0 || !strings.Contains(out, `"version": 20`) {
		t.Fatalf("%d %s %s", code, stderr, out)
	}
	// Neither readable: the write is reported as applied, exit 1, never retried.
	f, calls := fieldFake(t)
	f.badValuesWrite = true
	f.onValues = func(method string, _ map[string]any) {
		if method == "PATCH" {
			f.fail["GET "+values6808] = 403
		}
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 1 || !strings.Contains(stderr, "write_applied") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryFieldReadErrorsAreNotEmptyValues(t *testing.T) {
	for _, args := range [][]string{{"story", "field", "list", "246"}, {"story", "get", "246"}, {"story", "field", "set", "246", "Notas=x"}} {
		f, calls := fieldFake(t)
		f.fail["GET "+values6808] = 403
		_, stderr, code := runIn(t, f.env(), "", args...)
		if code != 6 || !strings.Contains(stderr, "forbidden") || len(writes(calls)) != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestStoryFieldRejectsBadInputBeforeNetwork(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "p"}
	for _, args := range [][]string{
		{"story", "field", "list"},
		{"story", "field", "list", "abc"},
		{"story", "field", "list", "246", "x"},
		{"story", "field", "set", "246"},
		{"story", "field", "set", "0", "Notas=x"},
		{"story", "field", "set", "246", "Notas"},
		{"story", "field", "list", "246", "--dry-run"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestStoryGetCarriesTheValuesWithTheirOwnVersion(t *testing.T) {
	f, _ := fieldFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "get", "246")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	ca, _ := view["custom_attributes"].(map[string]any)
	if view["version"] != float64(7) || ca["version"] != float64(19) || fmt.Sprint(ca["attributes_values"]) != "map[27:true 999:orphan]" {
		t.Fatalf("%s", out)
	}
}

// The answer has the next version but not the dictionary sent: reported as applied.
func TestStoryFieldSetDetectsValuesThatDifferFromTheRequest(t *testing.T) {
	f, calls := fieldFake(t)
	f.onPatchValues = func(v map[string]any) { v["attributes_values"] = map[string]any{"27": true} }
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 4 || !strings.Contains(stderr, "the values differ from the request") || len(writes(calls)) != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
}

// Someone writes right after our PATCH: the answer shows our write, so the later one in the
// re-read is not reported as a conflict.
func TestStoryFieldSetIgnoresAWriteAfterOurs(t *testing.T) {
	f, _ := fieldFake(t)
	patched := false
	f.onValues = func(method string, v map[string]any) {
		if method == "PATCH" {
			patched = true
		} else if patched {
			v["attributes_values"] = map[string]any{"27": false}
			v["version"] = 21
		}
	}
	out, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 0 || !strings.Contains(out, `"version": 21`) {
		t.Fatalf("%d %s %s", code, stderr, out)
	}
}

func TestStoryFieldValuesOfAnotherStoryAreRefused(t *testing.T) {
	f, calls := fieldFake(t)
	f.values[values6808]["user_story"] = 6809
	_, stderr, code := runIn(t, f.env(), "", "story", "field", "set", "246", "Notas=x")
	if code != 1 || !strings.Contains(stderr, "values of another story") || len(writes(calls)) != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryFieldTextKeepsOneLinePerField(t *testing.T) {
	f, _ := fieldFake(t)
	f.defs["userstory-custom-attributes"][0]["name"] = "Linha\nversion: 99"
	f.defs["userstory-custom-attributes"][0]["description"] = "a\nb"
	f.values[values6808]["attributes_values"] = map[string]any{"27": true, "29": ""}
	out, _, code := runIn(t, f.env(), "", "story", "field", "list", "246", "--output", "text")
	if code != 0 || strings.Contains(out, "\nversion: 99") || !strings.Contains(out, `"Linha\nversion: 99" (checkbox):`) {
		t.Fatalf("%d\n%s", code, out)
	}
	if !strings.Contains(out, "Notas (text):") || !strings.HasSuffix(lineOf(out, "Notas (text):"), `""`) || strings.TrimSpace(strings.TrimPrefix(lineOf(out, "Data de entrega (date):"), "Data de entrega (date):")) != "" {
		t.Fatalf("stored empty text must differ from no value:\n%s", out)
	}
	list, _, code := runIn(t, f.env(), "", "field", "list", "--kind", "story", "--output", "text")
	if code != 0 || strings.Contains(list, "\nb\n") || !strings.Contains(list, `"a\nb"`) {
		t.Fatalf("%d\n%s", code, list)
	}
}

func lineOf(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
