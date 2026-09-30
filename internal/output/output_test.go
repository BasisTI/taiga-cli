package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDetectMode(t *testing.T) {
	cases := []struct {
		flag string
		tty  bool
		want Mode
	}{{"", true, Text}, {"", false, JSON}, {"json", true, JSON}, {"text", false, Text}}
	for _, c := range cases {
		got, err := DetectMode(c.flag, c.tty)
		if err != nil || got != c.want {
			t.Fatalf("DetectMode(%q,%v) = %v,%v", c.flag, c.tty, got, err)
		}
	}
	if _, err := DetectMode("yaml", true); err == nil {
		t.Fatal("expected error for yaml")
	}
}

func TestWriteErrorJSONEnvelope(t *testing.T) {
	var b bytes.Buffer
	e := &Error{Code: "version_conflict", Source: "api", Stage: "patch userstories/1", Cause: "changed", Recovery: "retry", Exit: ExitConflict}
	if err := WriteError(&b, JSON, e); err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("not json: %s", b.String())
	}
	if got["error"]["code"] != "version_conflict" || got["error"]["recovery"] != "retry" {
		t.Fatalf("envelope = %v", got)
	}
}

func TestWriteErrorText(t *testing.T) {
	var b bytes.Buffer
	_ = WriteError(&b, Text, &Error{Code: "not_found", Cause: "story 9 not found", Recovery: "check the ref", Exit: ExitNotFound})
	s := b.String()
	if !strings.Contains(s, "error [not_found]: story 9 not found") || !strings.Contains(s, "fix: check the ref") {
		t.Fatalf("text = %q", s)
	}
}

func TestAsErrorWrapsUnknown(t *testing.T) {
	e := AsError(errors.New("boom"))
	if e.Code != "unexpected" || e.Exit != ExitUnexpected || e.Cause != "boom" {
		t.Fatalf("got %+v", e)
	}
	orig := &Error{Code: "x", Exit: 6}
	if AsError(orig) != orig {
		t.Fatal("must return the same *Error")
	}
}

func TestWriteJSONKeepsUnicodeAndControlChars(t *testing.T) {
	var b bytes.Buffer
	_ = WriteJSON(&b, map[string]string{"s": "Ação <b>\u0001\t\"x\""})
	var back map[string]string
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || back["s"] != "Ação <b>\u0001\t\"x\"" {
		t.Fatalf("round trip failed: %s", b.String())
	}
	if strings.Contains(b.String(), "\\u003c") {
		t.Fatal("HTML escaping must be off")
	}
}
