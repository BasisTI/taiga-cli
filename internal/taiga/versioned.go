package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
)

func (c *Client) getObject(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path})
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("GET %s: expected a JSON object: %w", stagePath(path), err)
	}
	if _, ok := m["version"]; !ok {
		return nil, fmt.Errorf("GET %s: resource has no version field", stagePath(path))
	}
	return m, nil
}

func withVersion(patch map[string]any, version json.RawMessage) map[string]any {
	body := make(map[string]any, len(patch)+1)
	for k, v := range patch {
		body[k] = v
	}
	body["version"] = json.Number(bytes.TrimSpace(version))
	return body
}

// PrepareVersioned reads the resource and returns the body that would be sent (for --dry-run).
func (c *Client) PrepareVersioned(ctx context.Context, path string, patch map[string]any) (map[string]any, map[string]json.RawMessage, error) {
	cur, err := c.getObject(ctx, stagePath(path))
	if err != nil {
		return nil, nil, err
	}
	return withVersion(patch, cur["version"]), cur, nil
}

// WriteVersioned applies patch with optimistic concurrency and one guarded retry.
func (c *Client) WriteVersioned(ctx context.Context, method, path string, patch map[string]any, force bool) (*Response, error) {
	body, first, err := c.PrepareVersioned(ctx, stagePath(path), patch)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, Request{Method: method, Path: path, Body: body})
	var ae *APIError
	if err == nil || !errors.As(err, &ae) || !ae.IsVersionConflict() {
		return resp, err
	}
	second, err := c.getObject(ctx, stagePath(path))
	if err != nil {
		return nil, err
	}
	if !force {
		if changed := changedKeys(patch, first, second); len(changed) > 0 {
			return nil, &ConflictError{Method: method, Path: path, Fields: changed}
		}
	}
	resp, err = c.Do(ctx, Request{Method: method, Path: path, Body: withVersion(patch, second["version"])})
	if err != nil && errors.As(err, &ae) && ae.IsVersionConflict() {
		return nil, &ConflictError{Method: method, Path: path}
	}
	return resp, err
}

func changedKeys(patch map[string]any, a, b map[string]json.RawMessage) []string {
	var out []string
	for k := range patch {
		if k == "version" {
			continue
		}
		va, inA := a[k]
		vb, inB := b[k]
		if inA != inB || !jsonEqual(va, vb) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// jsonEqual compares two present JSON values semantically: object key order and whitespace do
// not matter and numbers compare exactly (UseNumber). Callers compare key presence first, so a
// missing field never equals an explicit null.
func jsonEqual(x, y json.RawMessage) bool {
	vx, okx := decodeJSON(x)
	vy, oky := decodeJSON(y)
	return okx && oky && reflect.DeepEqual(vx, vy)
}

func decodeJSON(raw json.RawMessage) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}
