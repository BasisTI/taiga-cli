package taiga

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
)

// APIError is a non-2xx response from Taiga.
type APIError struct {
	Status       int
	Method, Path string
	Body         []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, stagePath(e.Path), e.Status, truncate(e.Body))
}

// IsVersionConflict reports Taiga's optimistic-concurrency rejection: 409 or 412 (precondition
// failed), or 400 with a "version" key, which is what Taiga 6.7 answers in practice.
func (e *APIError) IsVersionConflict() bool {
	if e.Status == 409 || e.Status == 412 {
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

func (e *NetworkError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Method, stagePath(e.Path), transportCause(e.Err))
}
func (e *NetworkError) Unwrap() error { return e.Err }

// UnreadableBodyError is a write (POST, PATCH, PUT) that Taiga confirmed with a 2xx status
// whose body could not be read: the change is applied, so it must never look like a network
// error that is safe to repeat. The client never retries it.
type UnreadableBodyError struct {
	Method, Path string
	Status       int
	Header       http.Header
	Err          error
}

func (e *UnreadableBodyError) Error() string {
	return fmt.Sprintf("%s %s returned HTTP %d, but its body could not be read: %s", e.Method, stagePath(e.Path), e.Status, transportCause(e.Err))
}
func (e *UnreadableBodyError) Unwrap() error { return e.Err }

// ConflictError means another writer changed the fields we are updating.
type ConflictError struct {
	Method, Path string
	Fields       []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s: version conflict on %v", e.Method, stagePath(e.Path), e.Fields)
}

// stagePath drops any query or fragment from an API path: their values may carry secrets
// and must never reach the error envelope.
func stagePath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		return p[:i]
	}
	return p
}

// transportCause describes a transport failure without the request URL (which carries the query).
func transportCause(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
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
		return &output.Error{Code: "network_error", Source: "network", Stage: ne.Method + " " + stagePath(ne.Path), Cause: transportCause(ne.Err), Recovery: "check connectivity to the Taiga URL (sandboxed agents need network access)", Exit: output.ExitNetwork}
	}
	var ue *UnreadableBodyError
	if errors.As(err, &ue) {
		return &output.Error{Code: "write_applied", Source: "network", Stage: ue.Method + " " + stagePath(ue.Path),
			Cause:    fmt.Sprintf("the change was applied (%s %s returned HTTP %d), but its answer could not be read: %s", ue.Method, stagePath(ue.Path), ue.Status, transportCause(ue.Err)),
			Recovery: "do not repeat the request: the change is already saved; read the resource to see its current state", Exit: output.ExitUnexpected}
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		cause := "resource changed concurrently"
		if len(ce.Fields) > 0 {
			cause = fmt.Sprintf("fields changed by someone else: %v", ce.Fields)
		}
		return &output.Error{Code: "version_conflict", Source: "api", Stage: ce.Method + " " + stagePath(ce.Path), Cause: cause, Recovery: "re-read the resource and retry; use --force-version to override", Exit: output.ExitConflict}
	}
	var lw *LocalWriteError
	if errors.As(err, &lw) {
		return &output.Error{Code: "local_write_failed", Source: "file", Cause: lw.Error(),
			Recovery: "check free space and permissions at the destination (or the reader of stdout); nothing was saved", Exit: output.ExitUnexpected}
	}
	var uu *UntrustedURLError
	if errors.As(err, &uu) {
		return &output.Error{Code: "attachment_url_untrusted", Source: "api", Stage: "GET " + uu.URL, Cause: uu.Error(),
			Recovery: "do not retry: the server returned a download URL outside the Taiga URL; check the MEDIA_URL of the instance and the --url in use", Exit: output.ExitUnexpected}
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		return output.AsError(err)
	}
	e := &output.Error{Source: "api", Stage: ae.Method + " " + stagePath(ae.Path), Cause: truncate(ae.Body)}
	switch {
	case ae.Status == 401:
		e.Code, e.Exit, e.Recovery = "auth_rejected", output.ExitAuth, "run `taiga auth status --diagnose`"
	case ae.Status == 403:
		e.Code, e.Exit, e.Recovery = "forbidden", output.ExitForbidden, "the account lacks permission for this operation"
	case ae.Status == 404:
		e.Code, e.Exit = "not_found", output.ExitNotFound
	case ae.Status == 413:
		e.Code, e.Exit, e.Recovery = "payload_too_large", output.ExitUsage, "the proxy in front of Taiga refuses a request this large (the Basis proxy accepts up to 50 MB); send a smaller file"
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
