package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
)

func (c *Client) getObject(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path})
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("GET %s: expected a JSON object: %w", path, err)
	}
	if _, ok := m["version"]; !ok {
		return nil, fmt.Errorf("GET %s: resource has no version field", path)
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
	cur, err := c.getObject(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	return withVersion(patch, cur["version"]), cur, nil
}

// WriteVersioned applies patch with optimistic concurrency and one guarded retry.
func (c *Client) WriteVersioned(ctx context.Context, method, path string, patch map[string]any, force bool) (*Response, error) {
	body, first, err := c.PrepareVersioned(ctx, path, patch)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, Request{Method: method, Path: path, Body: body})
	var ae *APIError
	if err == nil || !errors.As(err, &ae) || !ae.IsVersionConflict() {
		return resp, err
	}
	second, err := c.getObject(ctx, path)
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
		if !jsonEqual(a[k], b[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func jsonEqual(x, y json.RawMessage) bool {
	var bx, by bytes.Buffer
	if len(x) > 0 && json.Compact(&bx, x) != nil {
		return false
	}
	if len(y) > 0 && json.Compact(&by, y) != nil {
		return false
	}
	return bytes.Equal(bx.Bytes(), by.Bytes())
}
