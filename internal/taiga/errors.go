package taiga

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BasisTI/taiga-cli/internal/output"
)

// APIError is a non-2xx response from Taiga.
type APIError struct {
	Status       int
	Method, Path string
	Body         []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, truncate(e.Body))
}

// IsVersionConflict reports Taiga's optimistic-concurrency rejection (409, or 400 with a "version" key).
func (e *APIError) IsVersionConflict() bool {
	if e.Status == 409 {
		return true
	}
	if e.Status != 400 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(e.Body, &m) != nil {
		return false
	}
	_, ok := m["version"]
	return ok
}

// NetworkError wraps transport failures.
type NetworkError struct {
	Method, Path string
	Err          error
}

func (e *NetworkError) Error() string { return fmt.Sprintf("%s %s: %v", e.Method, e.Path, e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

// ConflictError means another writer changed the fields we are updating.
type ConflictError struct {
	Method, Path string
	Fields       []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s: version conflict on %v", e.Method, e.Path, e.Fields)
}

func truncate(b []byte) string {
	if len(b) > 2000 {
		return string(b[:2000]) + "…"
	}
	return string(b)
}

// ToOutput maps client errors to the stable error envelope.
func ToOutput(err error) *output.Error {
	var oe *output.Error
	if errors.As(err, &oe) {
		return oe
	}
	var ne *NetworkError
	if errors.As(err, &ne) {
		return &output.Error{Code: "network_error", Source: "network", Stage: ne.Method + " " + ne.Path, Cause: ne.Err.Error(), Recovery: "check connectivity to the Taiga URL (sandboxed agents need network access)", Exit: output.ExitNetwork}
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		cause := "resource changed concurrently"
		if len(ce.Fields) > 0 {
			cause = fmt.Sprintf("fields changed by someone else: %v", ce.Fields)
		}
		return &output.Error{Code: "version_conflict", Source: "api", Stage: ce.Method + " " + ce.Path, Cause: cause, Recovery: "re-read the resource and retry; use --force-version to override", Exit: output.ExitConflict}
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		return output.AsError(err)
	}
	e := &output.Error{Source: "api", Stage: ae.Method + " " + ae.Path, Cause: truncate(ae.Body)}
	switch {
	case ae.Status == 401:
		e.Code, e.Exit, e.Recovery = "auth_rejected", output.ExitAuth, "run `taiga auth status --diagnose`"
	case ae.Status == 403:
		e.Code, e.Exit, e.Recovery = "forbidden", output.ExitForbidden, "the account lacks permission for this operation"
	case ae.Status == 404:
		e.Code, e.Exit = "not_found", output.ExitNotFound
	case ae.IsVersionConflict():
		e.Code, e.Exit, e.Recovery = "version_conflict", output.ExitConflict, "re-read the resource and retry; with `taiga api`, include \"version\" or use --auto-version"
	case ae.Status >= 300 && ae.Status < 400:
		e.Code, e.Exit, e.Recovery = "unexpected_redirect", output.ExitNetwork, "check the Taiga URL (use the canonical https:// address)"
	case ae.Status >= 500:
		e.Code, e.Exit = "server_error", output.ExitNetwork
	default:
		e.Code, e.Exit = "invalid_request", output.ExitUsage
	}
	return e
}
