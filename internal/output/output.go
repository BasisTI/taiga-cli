// Package output renders results and errors as text or JSON and defines exit codes.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Mode int

const (
	Text Mode = iota
	JSON
)

const (
	ExitOK         = 0
	ExitUnexpected = 1
	ExitUsage      = 2
	ExitAuth       = 3
	ExitConflict   = 4
	ExitNotFound   = 5
	ExitForbidden  = 6
	ExitNetwork    = 7
)

// DetectMode picks the output mode: explicit flag wins, otherwise text on a TTY and JSON elsewhere.
func DetectMode(flag string, isTTY bool) (Mode, error) {
	switch flag {
	case "json":
		return JSON, nil
	case "text":
		return Text, nil
	case "":
		if isTTY {
			return Text, nil
		}
		return JSON, nil
	}
	return Text, &Error{Code: "usage", Cause: fmt.Sprintf("invalid --output %q", flag), Recovery: "use --output json or --output text", Exit: ExitUsage}
}

// Error is the stable error envelope. Cause must never contain secrets.
type Error struct {
	Code     string `json:"code"`
	Source   string `json:"source,omitempty"`
	Stage    string `json:"stage,omitempty"`
	Cause    string `json:"cause,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Exit     int    `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause == "" {
		return e.Code
	}
	return e.Code + ": " + e.Cause
}

// AsError returns err as *Error, wrapping unknown errors as "unexpected".
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "unexpected", Cause: err.Error(), Exit: ExitUnexpected}
}

// WriteError renders the error envelope.
func WriteError(w io.Writer, m Mode, e *Error) error {
	if m == JSON {
		return WriteJSON(w, map[string]*Error{"error": e})
	}
	s := fmt.Sprintf("error [%s]: %s\n", e.Code, e.Cause)
	if e.Stage != "" {
		s += fmt.Sprintf("  at: %s\n", e.Stage)
	}
	if e.Recovery != "" {
		s += fmt.Sprintf("  fix: %s\n", e.Recovery)
	}
	_, err := io.WriteString(w, s)
	return err
}

// WriteJSON writes v as indented JSON without HTML escaping.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type Field struct{ Key, Value string }

// WriteFields prints aligned "key: value" lines for text mode.
func WriteFields(w io.Writer, fields []Field) error {
	width := 0
	for _, f := range fields {
		if len(f.Key) > width {
			width = len(f.Key)
		}
	}
	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width+1, f.Key+":", f.Value); err != nil {
			return err
		}
	}
	return nil
}
