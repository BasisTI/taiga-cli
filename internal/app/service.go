// Package app resolves the selected project and names into ids and computes minimal writes
// for the curated commands. It talks to Taiga only through the narrow API interface.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// Object is a decoded Taiga object; numbers stay json.Number and unknown keys are kept.
type Object map[string]any

// API is the part of *taiga.Client the services use.
type API interface {
	Do(context.Context, taiga.Request) (*taiga.Response, error)
	GetAll(context.Context, string, url.Values) ([]json.RawMessage, error)
	WriteVersioned(context.Context, string, string, map[string]any, bool) (*taiga.Response, error)
	WriteVersionedFrom(context.Context, string, string, map[string]any, map[string]json.RawMessage, bool) (*taiga.Response, error)
	Upload(ctx context.Context, path string, fields map[string]string, fileField, fileName string, r io.Reader, size int64) (*taiga.Response, error)
	Download(ctx context.Context, rawURL string, w io.Writer) (int64, error)
	BaseURL() string
}

// Service is created per command run; catalogs are cached only for that run.
type Service struct {
	API      API
	Project  Object
	catalogs map[string][]Object
}

func Decode(b []byte) (Object, error) {
	var out Object
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return out, nil
}

// ID returns v as a positive id, or 0 when it is not an integer.
func ID(v any) int64 {
	n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
	return n
}

func Read(ctx context.Context, api API, path string, q url.Values) (Object, error) {
	r, err := api.Do(ctx, taiga.Request{Method: "GET", Path: path, Query: q})
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	return Decode(r.Body)
}

// New resolves project (slug, or numeric id) and returns a service bound to it.
func New(ctx context.Context, api API, project string) (*Service, error) {
	if project == "" {
		return nil, &output.Error{Code: "usage", Cause: "no project selected", Recovery: "pass --project, set TAIGA_PROJECT or add project to .taiga.toml", Exit: output.ExitUsage}
	}
	path, q := "projects/by_slug", url.Values{"slug": {project}}
	if n, err := strconv.ParseInt(project, 10, 64); err == nil && n > 0 {
		path, q = fmt.Sprintf("projects/%d", n), nil
	}
	p, err := Read(ctx, api, path, q)
	if err != nil {
		return nil, err
	}
	if ID(p["id"]) <= 0 || p["slug"] == nil {
		return nil, fmt.Errorf("invalid project response")
	}
	return &Service{API: api, Project: p, catalogs: map[string][]Object{}}, nil
}

func Usage(cause string) error {
	return &output.Error{Code: "usage", Cause: cause, Exit: output.ExitUsage}
}

// Unsupported reports a flag whose Taiga contract the CLI cannot honour safely.
func Unsupported(cause, recovery string) error {
	return &output.Error{Code: "unsupported_operation", Cause: cause, Recovery: recovery, Exit: output.ExitUsage}
}

func (s *Service) projectID() string { return fmt.Sprint(s.Project["id"]) }

// Catalog lists path for the project once per run. Entries that declare another project are
// dropped: a query parameter the server ignores must not widen the scope.
func (s *Service) Catalog(ctx context.Context, path string) ([]Object, error) {
	if items, ok := s.catalogs[path]; ok {
		return items, nil
	}
	items, err := s.list(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	s.catalogs[path] = items
	return items, nil
}

// list reads path for the project with the extra query parameters, uncached, dropping entries
// that declare another project.
func (s *Service) list(ctx context.Context, path string, extra url.Values) ([]Object, error) {
	q := url.Values{"project": {s.projectID()}}
	for k, v := range extra {
		q[k] = v
	}
	raws, err := s.API.GetAll(ctx, path, q)
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	items := []Object{}
	for _, raw := range raws {
		obj, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if p, ok := obj["project"]; ok && fmt.Sprint(p) != s.projectID() {
			continue
		}
		items = append(items, obj)
	}
	return items, nil
}

// Resolve finds exactly one entry whose nameKey or id equals selector.
func Resolve(items []Object, selector, nameKey string) (Object, error) {
	return resolve(items, selector, nameKey, true)
}

func resolve(items []Object, selector, nameKey string, byID bool) (Object, error) {
	matches := []Object{}
	for _, o := range items {
		if fmt.Sprint(o[nameKey]) == selector || (byID && ID(o["id"]) > 0 && fmt.Sprint(o["id"]) == selector) {
			matches = append(matches, o)
		}
	}
	if len(matches) == 0 {
		return nil, &output.Error{Code: "not_found", Cause: "catalog entry not found: " + selector, Exit: output.ExitNotFound}
	}
	if len(matches) != 1 {
		return nil, &output.Error{Code: "ambiguous_name", Cause: "multiple catalog entries: " + selector, Recovery: "use the id instead of the name", Exit: output.ExitUsage}
	}
	return matches[0], nil
}

// Snapshot converts an object read from Taiga back to raw JSON, for WriteVersionedFrom.
func Snapshot(o Object) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	err = json.Unmarshal(b, &raw)
	return raw, err
}

// WritePlan is what --dry-run prints instead of sending.
type WritePlan struct {
	DryRun bool   `json:"dry_run"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   Object `json:"body"`
}

// Write sends patch against before (the read used to compute it) and re-reads the resource.
// An empty patch writes nothing and returns before.
func (s *Service) Write(ctx context.Context, k kind, path string, before, patch Object, dry, force bool) (any, error) {
	if len(patch) == 0 {
		return before, nil
	}
	if _, ok := before["version"]; !ok {
		return nil, fmt.Errorf("resource has no version: %s", path)
	}
	if dry {
		body := Object{}
		for k, v := range patch {
			body[k] = v
		}
		body["version"] = before["version"]
		return WritePlan{true, "PATCH", path, body}, nil
	}
	resp, err := s.send(ctx, k, path, before, patch, force)
	if err != nil && uncertain(err) {
		return s.confirmPatch(ctx, k, path, patch, 0, err, fmt.Sprintf("`taiga %s get %v`", k.name, before["ref"]))
	}
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	o, err := reread(ctx, s.API, "PATCH", path, path, resp)
	return o, k.applied(err, fmt.Sprintf("`taiga %s get %v`", k.name, before["ref"]))
}

// send writes patch with the version of before: once for opaque fields of a story, else with
// the guarded retry. The error is the client's, so the caller can tell an unknown outcome.
func (s *Service) send(ctx context.Context, k kind, path string, before, patch Object, force bool) (*taiga.Response, error) {
	if k.fenceAssignees && opaque(patch) && !force {
		return s.writeOnce(ctx, path, before, patch)
	}
	raw, err := Snapshot(before)
	if err != nil {
		return nil, err
	}
	return confirmed(s.API.WriteVersionedFrom(ctx, "PATCH", path, patch, raw, force))
}

// uncertain is a failed write that Taiga may have applied: the request left (the connection
// opened) and no answer said what happened: a network error, a 5xx, or a 3xx, which the client
// never follows and a proxy may have answered after passing the request on.
func uncertain(err error) bool {
	var ae *taiga.APIError
	return !taiga.NotSent(err) && (unknownOutcome(err) || errors.As(err, &ae) && ae.Status >= 300 && ae.Status < 400)
}

// confirmPatch decides a PATCH whose outcome is unknown by re-reading the resource: when every
// field sent shows the value asked for, the write landed (they all differed before it).
// Otherwise <kind>_update_unconfirmed, exit 1: a request still running on the server can land
// after the check, and a re-run would repeat a merge like --append-description. A resource
// without OCC passes version, the next version of the read the patch came from: any other
// version means another write landed next to ours, which a matching re-read cannot rule out.
// check is the command that shows the resource, for the recovery.
func (s *Service) confirmPatch(ctx context.Context, k kind, path string, patch Object, version int64, sendErr error, check string) (Object, error) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	now, err := Read(rctx, s.API, path, nil)
	checked := "the re-read does not show the change, but the request may still be running on the server"
	if err != nil {
		checked = "the re-read to check failed: " + output.AsError(err).Error()
	} else if shows(now, patch) {
		if version == 0 || ID(now["version"]) == version {
			return now, nil
		}
		checked = fmt.Sprintf("the re-read shows the values sent, but with version %v instead of %d: another write landed next to it and may have been overwritten", now["version"], version)
	}
	return nil, &output.Error{Code: k.name + "_update_unconfirmed", Source: taiga.ToOutput(sendErr).Source, Stage: "PATCH " + path,
		Cause:    fmt.Sprintf("the change may have been applied: PATCH %s failed (%v) and %s", path, sendErr, checked),
		Recovery: "do not re-run the command blindly: wait, check with " + check + " and repeat only what is still missing",
		Exit:     output.ExitUnexpected}
}

// shows reports whether o holds every field of patch with the value sent (tags by name).
func shows(o, patch Object) bool { return len(differences(o, patch)) == 0 }

// differences lists, sorted, the fields of patch that o does not hold with the value sent.
// Tags compare by name, as Taiga stores them (the CLI already sends them in lower case).
func differences(o, patch Object) []string {
	out := []string{}
	for key, want := range patch {
		got := o[key]
		if key == "tags" {
			names, err := Names(got)
			if err != nil {
				out = append(out, key)
				continue
			}
			got = names
		}
		if _, ok := o[key]; !ok || !equal(got, want) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// applied gives write_applied the recovery of the kind: the generic one names stories.
func (k kind) applied(err error, check string) error {
	var e *output.Error
	if k == storyKind || !errors.As(err, &e) || e.Code != "write_applied" {
		return err
	}
	out := *e
	out.Recovery = "do not re-run the command: the change is already saved; check it with " + check
	return &out
}

// opaqueKeys are fields whose answer does not show everything a write replaces: Taiga answers
// assigned_users as the stored list plus assigned_to (docs/api-notes.md). Comparing them after a
// version conflict can miss a concurrent change, so a patch with any of them is never retried.
var opaqueKeys = []string{"assigned_users", "assigned_to"}

func opaque(patch Object) bool {
	for _, k := range opaqueKeys {
		if _, ok := patch[k]; ok {
			return true
		}
	}
	return false
}

// writeOnce sends patch with the version of before and turns a version conflict into an error.
func (s *Service) writeOnce(ctx context.Context, path string, before, patch Object) (*taiga.Response, error) {
	body := map[string]any{}
	for k, v := range patch {
		body[k] = v
	}
	body["version"] = before["version"]
	resp, err := confirmed(s.API.Do(ctx, taiga.Request{Method: "PATCH", Path: path, Body: body}))
	var ae *taiga.APIError
	if err != nil && errors.As(err, &ae) && ae.IsVersionConflict() {
		return nil, &taiga.ConflictError{Method: "PATCH", Path: path, Fields: opaqueKeys}
	}
	return resp, err
}

// confirmed turns a PATCH whose 2xx answer was lost (*taiga.UnreadableBodyError) into a
// response without body, so it takes the path of an answer that does not decode: re-read,
// postcondition and, if the re-read fails too, write_applied.
func confirmed(resp *taiga.Response, err error) (*taiga.Response, error) {
	var ue *taiga.UnreadableBodyError
	if errors.As(err, &ue) {
		return &taiga.Response{Status: ue.Status, Header: ue.Header}, nil
	}
	return resp, err
}

// reread returns the resource after a write that Taiga confirmed with a 2xx status. If the GET
// fails, the write response stands in for it; if that does not decode either, the error says
// the change was applied, because re-running the command would repeat it.
func reread(ctx context.Context, api API, method, writePath, readPath string, written *taiga.Response) (Object, error) {
	o, err := Read(ctx, api, readPath, nil)
	if err == nil {
		return o, nil
	}
	if w, derr := Decode(written.Body); derr == nil {
		return w, nil
	}
	return nil, WriteApplied(method, writePath, written.Status, err)
}

// WriteApplied reports a write confirmed by its HTTP status whose result could not be read.
// It exits 1, not 7: scripts that retry network errors must not repeat an applied write.
func WriteApplied(method, path string, status int, readErr error) error {
	read := output.AsError(readErr)
	cause := fmt.Sprintf("the change was applied (%s %s returned HTTP %d), but its result could not be read: %s", method, path, status, read.Error())
	if read.Stage != "" {
		cause += " (at " + read.Stage + ")"
	}
	return &output.Error{Code: "write_applied", Source: read.Source, Stage: fmt.Sprintf("%s %s", method, path), Cause: cause,
		Recovery: "do not re-run the command: the change is already saved; check the story with `taiga story get` or `taiga story list`", Exit: output.ExitUnexpected}
}
