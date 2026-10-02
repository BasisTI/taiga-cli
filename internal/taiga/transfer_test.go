package taiga

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func upload(c *Client, content string) (*Response, error) {
	return c.Upload(context.Background(), "userstories/attachments", map[string]string{"project": "37", "object_id": "6808", "description": "a \"quoted\" note"},
		"attached_file", `relatório "final".txt`, strings.NewReader(content), int64(len(content)))
}

func TestUploadSendsMultipartOnce(t *testing.T) {
	content := "line one\r\n--not-a-boundary\x00\xff"
	var hits atomic.Int32
	status := 201
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/userstories/attachments" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") || r.ContentLength <= int64(len(content)) {
			t.Errorf("content-type=%q length=%d", r.Header.Get("Content-Type"), r.ContentLength)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		} else {
			for k, want := range map[string]string{"project": "37", "object_id": "6808", "description": "a \"quoted\" note"} {
				if got := r.FormValue(k); got != want {
					t.Errorf("%s=%q", k, got)
				}
			}
			f, h, err := r.FormFile("attached_file")
			if err != nil {
				t.Errorf("file: %v", err)
			} else {
				b, _ := io.ReadAll(f)
				if string(b) != content || h.Filename != `relatório "final".txt` {
					t.Errorf("file %q = %q", h.Filename, b)
				}
			}
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, `{"id":1}`)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}, WithRetryWait(0))
	resp, err := upload(c, content)
	if err != nil || resp.Status != 201 || string(resp.Body) != `{"id":1}` || hits.Load() != 1 {
		t.Fatalf("resp=%v err=%v hits=%d", resp, err, hits.Load())
	}
	// A 5xx is never repeated: the file may already be stored.
	hits.Store(0)
	status = 500
	_, err = upload(c, content)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 500 || hits.Load() != 1 {
		t.Fatalf("500: err=%v hits=%d", err, hits.Load())
	}
}

func TestUploadTruncatedAnswerIsUnreadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(201)
		_, _ = fmt.Fprint(w, `{"id":`)
	}))
	defer srv.Close()
	_, err := upload(New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}), "x")
	var ue *UnreadableBodyError
	var ne *NetworkError
	if !errors.As(err, &ue) || ue.Status != 201 || errors.As(err, &ne) {
		t.Fatalf("%T %v", err, err)
	}
}

func TestUploadConnectionRefusedIsNotSent(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	_, err = upload(New("http://"+addr, StaticToken{Type: "Bearer", Value: "tok"}), "x")
	var ne *NetworkError
	if !errors.As(err, &ne) || !NotSent(err) {
		t.Fatalf("%T %v", err, err)
	}
}

// A reader that yields fewer bytes than announced must not produce a complete upload.
func TestUploadShortFileFailsBeforeAnswer(t *testing.T) {
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			got.Add(1)
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"})
	_, err := c.Upload(context.Background(), "userstories/attachments", nil, "attached_file", "a.txt", strings.NewReader("abc"), 10)
	if err == nil || got.Load() != 0 {
		t.Fatalf("err=%v complete bodies=%d", err, got.Load())
	}
}

func TestUpload413IsPayloadTooLarge(t *testing.T) {
	page := "<html><head><title>413 Request Entity Too Large</title></head></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(413)
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()
	_, err := upload(New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}), strings.Repeat("x", 1<<16))
	e := ToOutput(err)
	if e.Code != "payload_too_large" || e.Exit != output.ExitUsage || !strings.Contains(e.Cause, page) || e.Recovery == "" {
		t.Fatalf("%+v", e)
	}
}

func TestDownloadRejectsOtherOrigin(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"})
	host := strings.TrimPrefix(srv.URL, "http://")
	_, port, _ := net.SplitHostPort(host)
	for _, raw := range []string{
		"http://evil.test:" + port + "/media/a?token=secret",
		"http://127.0.0.1:1/media/a?token=secret",
		"https://" + host + "/media/a?token=secret",
		"http://user@" + host + "/media/a?token=secret",
		"/media/a?token=secret",
		"http://" + host + "@evil.test/media/a?token=secret",
	} {
		var buf bytes.Buffer
		_, err := c.Download(context.Background(), raw, &buf)
		e := ToOutput(err)
		if e.Code != "attachment_url_untrusted" || e.Exit != output.ExitUnexpected || strings.Contains(e.Error(), "secret") {
			t.Fatalf("%s: %+v", raw, e)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("requests: %d", hits.Load())
	}
}

func TestDownloadSameOriginIgnoresCaseAndDefaultPort(t *testing.T) {
	if !sameOrigin("https://Agile.Basis.com.br", "https://agile.basis.com.br:443/media/a") || !sameOrigin("http://h", "http://H:80/x") {
		t.Fatal("equivalent origins refused")
	}
	if sameOrigin("http://h", "http://h:8000/x") || sameOrigin("https://h", "http://h:443/x") {
		t.Fatal("different origins accepted")
	}
}

func TestDownloadSendsNoAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("credentials sent: %v", r.Header)
		}
		_, _ = fmt.Fprint(w, "bytes")
	}))
	defer srv.Close()
	var buf bytes.Buffer
	n, err := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}).Download(context.Background(), srv.URL+"/media/a.txt?token=t", &buf)
	if err != nil || n != 5 || buf.String() != "bytes" {
		t.Fatalf("n=%d err=%v body=%q", n, err, buf.String())
	}
}

func TestDownloadDropsFragment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/media/a b.txt" || r.URL.RawQuery != "token=t%3Ax" || r.URL.Fragment != "" || strings.Contains(r.RequestURI, "#") {
			t.Errorf("uri=%q", r.RequestURI)
		}
	}))
	defer srv.Close()
	if _, err := New(srv.URL, nil).Download(context.Background(), srv.URL+"/media/a%20b.txt?token=t%3Ax#_taiga-refresh=userstory:1", io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadDoesNotFollowRedirect(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	_, err := New(srv.URL, nil).Download(context.Background(), srv.URL+"/media/a?token=secret", io.Discard)
	e := ToOutput(err)
	if e.Code != "unexpected_redirect" || hits.Load() != 1 || strings.Contains(e.Error(), "secret") {
		t.Fatalf("hits=%d %+v", hits.Load(), e)
	}
}

func TestDownloadRetriesOnlyBeforeFirstByte(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(502)
			return
		}
		_, _ = fmt.Fprint(w, "content")
	}))
	defer srv.Close()
	var buf bytes.Buffer
	if n, err := New(srv.URL, nil, WithRetryWait(0)).Download(context.Background(), srv.URL+"/media/a", &buf); err != nil || n != 7 || buf.String() != "content" || hits.Load() != 2 {
		t.Fatalf("n=%d err=%v body=%q hits=%d", n, err, buf.String(), hits.Load())
	}

	// A body cut in the middle is a network error, and the download is not tried again:
	// part of it is already written.
	hits.Store(0)
	cutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "1000")
		_, _ = fmt.Fprint(w, "partial")
	}))
	defer cutSrv.Close()
	buf.Reset()
	n, err := New(cutSrv.URL, nil, WithRetryWait(0)).Download(context.Background(), cutSrv.URL+"/media/a?token=secret", &buf)
	var ne *NetworkError
	if !errors.As(err, &ne) || hits.Load() != 1 || n != int64(len("partial")) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("n=%d err=%v hits=%d", n, err, hits.Load())
	}
}

func TestDownloadErrorStatusKeepsTokenOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	_, err := New(srv.URL, nil).Download(context.Background(), srv.URL+"/media/a?token=secret", io.Discard)
	e := ToOutput(err)
	if e.Code != "forbidden" || strings.Contains(e.Error(), "secret") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("%+v", e)
	}
}

// Transfers have no total timeout of their own: the context bounds them.
func TestTransferClientHasNoTotalTimeout(t *testing.T) {
	c := New("http://h", nil, WithHTTPClient(&http.Client{Timeout: 30}))
	tc := c.transferClient()
	if tc.Timeout != 0 || tc.CheckRedirect == nil {
		t.Fatalf("timeout=%v redirect=%v", tc.Timeout, tc.CheckRedirect != nil)
	}
	if tr, ok := tc.Transport.(*http.Transport); !ok || tr.ResponseHeaderTimeout != httpHeaderTimeout {
		t.Fatalf("transport %T", tc.Transport)
	}
	if c.http.Timeout != 30 {
		t.Fatal("the API client was changed")
	}
}

// An error page that echoes the request (URL, token) must not carry the token to the output.
func TestDownloadErrorBodyHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = fmt.Fprintf(w, "denied %s; raw %s; escaped %s", r.URL.String(), r.URL.Query().Get("token"), url.QueryEscape(r.URL.Query().Get("token")))
	}))
	defer srv.Close()
	_, err := New(srv.URL, nil).Download(context.Background(), srv.URL+"/media/a?token=SIG%3ANED&x=1", io.Discard)
	e := ToOutput(err)
	for _, s := range []string{e.Cause, e.Error(), err.Error()} {
		if strings.Contains(s, "SIG:NED") || strings.Contains(s, "SIG%3ANED") || strings.Contains(s, "NED") {
			t.Fatalf("token in %q", s)
		}
	}
	if e.Code != "forbidden" || !strings.Contains(e.Cause, "denied /media/a?") {
		t.Fatalf("%+v", e)
	}
}

type fullWriter struct{}

func (fullWriter) Write([]byte) (int, error) { return 0, syscall.ENOSPC }

// A full disk is a local failure: no new GET, no network_error.
func TestDownloadLocalWriteErrorIsNotNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = fmt.Fprint(w, "bytes")
	}))
	defer srv.Close()
	_, err := New(srv.URL, nil, WithRetryWait(0)).Download(context.Background(), srv.URL+"/media/a", fullWriter{})
	e := ToOutput(err)
	if hits.Load() != 1 || e.Code != "local_write_failed" || e.Source != "file" || e.Exit != output.ExitUnexpected || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("hits=%d %+v", hits.Load(), e)
	}
}

func TestRedactSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"http://h/media/a?token=abc#frag":       "http://h/media/a?…#frag",
		"see http://h/x?a=1&token=abc and more": "see http://h/x?… and more",
		"token=abc&other=1":                     "token=…&other=1",
		"no secrets here?":                      "no secrets here?",
	} {
		if got := RedactSecrets(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
