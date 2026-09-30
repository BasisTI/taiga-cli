package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// GetAll returns every item of a list endpoint, asking Taiga to disable pagination and following pages if it paginates anyway.
func (c *Client) GetAll(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	var all []json.RawMessage
	for page := 1; page <= 1000; page++ {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = append([]string(nil), v...)
		}
		if page > 1 {
			qq.Set("page", strconv.Itoa(page))
		}
		resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: qq, Header: http.Header{"x-disable-pagination": {"True"}}})
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(resp.Body, &items); err != nil {
			return nil, fmt.Errorf("GET %s: expected a JSON array: %w", stagePath(path), err)
		}
		all = append(all, items...)
		if resp.Header.Get("x-pagination-next") == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("GET %s: more than 1000 pages", stagePath(path))
}
