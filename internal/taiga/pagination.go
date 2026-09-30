package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetAll returns every item of a list endpoint, asking Taiga to disable pagination and, if it
// paginates anyway, following the page number announced in X-Pagination-Next.
func (c *Client) GetAll(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	all := []json.RawMessage{}
	current := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil {
		current = p
	}
	page := q.Get("page")
	for n := 0; n < 1000; n++ {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = append([]string(nil), v...)
		}
		if page != "" {
			qq.Set("page", page)
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
		next := resp.Header.Get("x-pagination-next")
		if next == "" {
			return all, nil
		}
		p, err := c.nextPage(next, current)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", stagePath(path), err)
		}
		current, page = p, strconv.Itoa(p)
	}
	return nil, fmt.Errorf("GET %s: more than 1000 pages", stagePath(path))
}

// nextPage extracts the page number from an X-Pagination-Next link. The link must point to the
// client's own host and move forward; its URL is never echoed (it may carry query values).
func (c *Client) nextPage(next string, current int) (int, error) {
	base, _ := url.Parse(c.base)
	u, err := url.Parse(next)
	if err != nil || base == nil || !strings.EqualFold(u.Host, base.Host) {
		return 0, fmt.Errorf("pagination link points outside the Taiga URL")
	}
	p, err := strconv.Atoi(u.Query().Get("page"))
	if err != nil || p <= current {
		return 0, fmt.Errorf("pagination link has no page after %d", current)
	}
	return p, nil
}
