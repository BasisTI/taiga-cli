// Package taiga is an HTTP client for the Taiga REST API v1.
package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Token struct{ Type, Value string }

type TokenSource interface {
	Token(ctx context.Context) (Token, error)
}

// StaticToken is a fixed token (TAIGA_TOKEN, tests).
type StaticToken Token

func (s StaticToken) Token(context.Context) (Token, error) { return Token(s), nil }

type Client struct {
	base      string
	http      *http.Client
	tokens    TokenSource
	retryWait time.Duration
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithRetryWait(d time.Duration) Option { return func(c *Client) { c.retryWait = d } }

// New builds a client for baseURL (scheme://host, already normalized).
func New(baseURL string, ts TokenSource, opts ...Option) *Client {
	c := &Client{base: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}, tokens: ts, retryWait: 500 * time.Millisecond}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) BaseURL() string { return c.base }

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	Header http.Header
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends one API request. GET is retried up to twice on network errors and 5xx.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	var payload []byte
	if r.Body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(r.Body); err != nil {
			return nil, err
		}
		payload = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	}
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	target := c.base + "/api/v1/" + strings.TrimPrefix(r.Path, "/")
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	attempts := 1
	if r.Method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && c.retryWait > 0 {
			select {
			case <-ctx.Done():
				return nil, &NetworkError{Method: r.Method, Path: r.Path, Err: ctx.Err()}
			case <-time.After(c.retryWait * time.Duration(i)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		if tok.Value != "" {
			req.Header.Set("Authorization", tok.Type+" "+tok.Value)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = &NetworkError{Method: r.Method, Path: r.Path, Err: err}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = &NetworkError{Method: r.Method, Path: r.Path, Err: err}
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = &APIError{Status: resp.StatusCode, Method: r.Method, Path: r.Path, Body: body}
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, &APIError{Status: resp.StatusCode, Method: r.Method, Path: r.Path, Body: body}
		}
		return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
	}
	return nil, lastErr
}
