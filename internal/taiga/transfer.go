package taiga

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// httpHeaderTimeout bounds the wait for the answer headers of a transfer once the request is
// sent. The transfer itself has no total timeout: the caller's context bounds it.
const httpHeaderTimeout = 30 * time.Second

// transferClient is a copy of the API client without a total timeout, for uploads and
// downloads that can take longer than an API call. Redirects stay refused.
func (c *Client) transferClient() *http.Client {
	h := *c.http
	h.Timeout = 0
	switch tr := h.Transport.(type) {
	case nil:
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ResponseHeaderTimeout = httpHeaderTimeout
		h.Transport = t
	case *http.Transport:
		t := tr.Clone()
		t.ResponseHeaderTimeout = httpHeaderTimeout
		h.Transport = t
	}
	return &h
}

// Upload sends one multipart POST with fields and the file read from r, which must yield
// exactly size bytes. Never retried: the file may be stored even when the answer is lost. The
// body is streamed with a known Content-Length (Django reads the body by it, so a chunked
// upload could arrive empty). A 2xx whose body cannot be read is *UnreadableBodyError.
func (c *Client) Upload(ctx context.Context, path string, fields map[string]string, fileField, fileName string, r io.Reader, size int64) (*Response, error) {
	var head, tail bytes.Buffer
	mw := multipart.NewWriter(&head)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := mw.WriteField(k, fields[k]); err != nil {
			return nil, err
		}
	}
	if _, err := mw.CreateFormFile(fileField, fileName); err != nil {
		return nil, err
	}
	// The closing boundary goes to its own buffer, so the file streams between the two.
	headBytes := bytes.Clone(head.Bytes())
	head.Reset()
	if err := mw.Close(); err != nil {
		return nil, err
	}
	tail.Write(head.Bytes())
	body := io.MultiReader(bytes.NewReader(headBytes), io.LimitReader(r, size), &tail)
	tok, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/"+strings.TrimPrefix(path, "/"), body)
	if err != nil {
		return nil, fmt.Errorf("POST %s: invalid request URL", stagePath(path))
	}
	req.ContentLength = int64(len(headBytes)) + size + int64(tail.Len())
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if tok.Value != "" {
		req.Header.Set("Authorization", tok.Type+" "+tok.Value)
	}
	resp, err := c.transferClient().Do(req)
	if err != nil {
		return nil, &NetworkError{Method: http.MethodPost, Path: path, Err: err}
	}
	answer, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil, &UnreadableBodyError{Method: http.MethodPost, Path: path, Status: resp.StatusCode, Header: resp.Header, Err: err}
		}
		return nil, &NetworkError{Method: http.MethodPost, Path: path, Err: err}
	}
	if resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Method: http.MethodPost, Path: path, Body: answer}
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: answer}, nil
}

func (c *Client) token(ctx context.Context) (Token, error) {
	if c.tokens == nil {
		return Token{}, nil
	}
	return c.tokens.Token(ctx)
}

// UntrustedURLError is a download URL from the server whose origin is not the client's. Its
// text never carries the query, which holds the signed token.
type UntrustedURLError struct {
	URL, Base string
}

func (e *UntrustedURLError) Error() string {
	return fmt.Sprintf("the attachment URL %s is not on %s", e.URL, e.Base)
}

// Download streams GET rawURL into w and returns the bytes written. rawURL must have the
// client's scheme, host and port: it comes from the server and carries a signed token, so it
// is never followed elsewhere. No Authorization header is sent (the token in the URL is the
// credential), the fragment is dropped and redirects are not followed. Network errors and 5xx
// are retried only while nothing has been written to w.
func (c *Client) Download(ctx context.Context, rawURL string, w io.Writer) (int64, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !sameOrigin(c.base, rawURL) {
		return 0, &UntrustedURLError{URL: redact(rawURL), Base: c.base}
	}
	u.Fragment, u.RawFragment = "", ""
	path := u.EscapedPath()
	hc := c.transferClient()
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 && c.retryWait > 0 {
			select {
			case <-ctx.Done():
				return 0, &NetworkError{Method: http.MethodGet, Path: path, Err: ctx.Err()}
			case <-time.After(c.retryWait * time.Duration(i)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return 0, &UntrustedURLError{URL: redact(rawURL), Base: c.base}
		}
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = &NetworkError{Method: http.MethodGet, Path: path, Err: err}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			// An error page may echo the request: the token must not reach the error output.
			lastErr = &APIError{Status: resp.StatusCode, Method: http.MethodGet, Path: path, Body: []byte(redactURLSecrets(string(body), u))}
			if resp.StatusCode >= 500 {
				continue
			}
			return 0, lastErr
		}
		dst := &localWriter{w: w}
		n, err := io.Copy(dst, resp.Body)
		_ = resp.Body.Close()
		if err == nil {
			return n, nil
		}
		if dst.err != nil {
			// The destination failed (full disk, closed pipe): fetching again would not help.
			return n, &LocalWriteError{Err: dst.err}
		}
		lastErr = &NetworkError{Method: http.MethodGet, Path: path, Err: err}
		if n > 0 {
			return n, lastErr
		}
	}
	return 0, lastErr
}

// localWriter records the error of the destination, to tell it from a transport error.
type localWriter struct {
	w   io.Writer
	err error
}

func (l *localWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	if err != nil {
		l.err = err
	}
	return n, err
}

// LocalWriteError is a download whose destination could not be written. Nothing is retried.
type LocalWriteError struct{ Err error }

func (e *LocalWriteError) Error() string { return "writing the downloaded file: " + e.Err.Error() }
func (e *LocalWriteError) Unwrap() error { return e.Err }

var (
	urlQuery   = regexp.MustCompile(`\?[^\s"'<>#]+`)
	tokenParam = regexp.MustCompile(`(?i)(token=)[^&\s"'<>#]*`)
)

// RedactSecrets hides the query of every URL in s, and any bare token= value: signed URLs
// carry their credential there.
func RedactSecrets(s string) string {
	s = urlQuery.ReplaceAllString(s, "?…")
	return tokenParam.ReplaceAllString(s, "${1}…")
}

// RedactTokens hides only the value of every token= parameter in s, keeping the rest of the
// text: read output keeps the URLs users wrote, but never a signed link's credential.
func RedactTokens(s string) string {
	return tokenParam.ReplaceAllString(s, "${1}…")
}

// redactURLSecrets is RedactSecrets plus every value of u's query, raw or escaped, wherever it
// appears in s.
func redactURLSecrets(s string, u *url.URL) string {
	for _, vs := range u.Query() {
		for _, v := range vs {
			if len(v) < 6 {
				continue
			}
			for _, form := range []string{v, url.QueryEscape(v), url.PathEscape(v)} {
				s = strings.ReplaceAll(s, form, "…")
			}
		}
	}
	return RedactSecrets(s)
}

// sameOrigin compares scheme, host (case-insensitive) and port (with the scheme's default)
// of base and raw. A URL with user information is never the same origin.
func sameOrigin(base, raw string) bool {
	b, err1 := url.Parse(base)
	u, err2 := url.Parse(raw)
	if err1 != nil || err2 != nil || u.User != nil || u.Opaque != "" || u.Host == "" {
		return false
	}
	origin := func(x *url.URL) string {
		port := x.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[strings.ToLower(x.Scheme)]
		}
		return strings.ToLower(x.Scheme) + "://" + net.JoinHostPort(strings.ToLower(x.Hostname()), port)
	}
	return origin(b) == origin(u)
}

// redact drops the query and fragment of a URL for messages.
func redact(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// NotSent is a network error raised before the request could leave: the connection was never
// opened (DNS failure, refused or unreachable). Nothing reached Taiga.
func NotSent(err error) bool {
	var ne *NetworkError
	if !errors.As(err, &ne) {
		return false
	}
	var dns *net.DNSError
	var op *net.OpError
	return errors.As(err, &dns) || errors.As(err, &op) && op.Op == "dial"
}
