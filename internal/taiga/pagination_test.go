package taiga

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestGetAllFollowsPagesAndSendsDisableHeader(t *testing.T) {
	var sawHeader bool
	var srv *httptest.Server
	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("x-disable-pagination") == "True"
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("x-pagination-next", srv.URL+"/api/v1/userstories?page=2")
			_, _ = fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			return
		}
		_, _ = fmt.Fprint(w, `[{"id":3}]`)
	}))
	srv.Start()
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "t"}, WithRetryWait(0))
	items, err := c.GetAll(context.Background(), "userstories", nil)
	if err != nil || len(items) != 3 || !sawHeader {
		t.Fatalf("items=%d err=%v header=%v", len(items), err, sawHeader)
	}
}

func TestGetAllRejectsNonArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"id":1}`) }))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	if _, err := c.GetAll(context.Background(), "userstories/1", nil); err == nil {
		t.Fatal("expected error for non-array")
	}
}

func TestGetAllErrorDoesNotEchoQueryInPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"id":1}`) }))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.GetAll(context.Background(), "userstories?token=SENTINEL", nil)
	if err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("err = %v", err)
	}
}

// pagedServer serves pages and records the page parameter of every request; next(page) is the
// X-Pagination-Next value to send for that page ("" ends the listing).
func pagedServer(t *testing.T, next func(base, page string) string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var pages []string
	var srv *httptest.Server
	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		pages = append(pages, page)
		mu.Unlock()
		if n := next(srv.URL, page); n != "" {
			w.Header().Set("X-Pagination-Next", n)
		}
		_, _ = fmt.Fprintf(w, `[{"page":%q}]`, page)
	}))
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &pages
}

func TestGetAllFollowsPageNumberFromNextHeader(t *testing.T) {
	srv, pages := pagedServer(t, func(base, page string) string {
		if page == "3" {
			return base + "/api/v1/userstories?page=4"
		}
		return ""
	})
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	items, err := c.GetAll(context.Background(), "userstories", url.Values{"page": {"3"}})
	if err != nil || len(items) != 2 || strings.Join(*pages, ",") != "3,4" {
		t.Fatalf("items=%d err=%v pages=%v", len(items), err, *pages)
	}
}

func TestGetAllRejectsNextHeaderOnAnotherHost(t *testing.T) {
	srv, pages := pagedServer(t, func(base, page string) string {
		return "https://evil.example/api/v1/userstories?page=2&token=SENTINEL"
	})
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.GetAll(context.Background(), "userstories", nil)
	if err == nil || len(*pages) != 1 || strings.Contains(err.Error(), "SENTINEL") || strings.Contains(err.Error(), "evil") {
		t.Fatalf("err=%v pages=%v", err, *pages)
	}
}

func TestGetAllRejectsNextHeaderThatDoesNotAdvance(t *testing.T) {
	srv, pages := pagedServer(t, func(base, page string) string {
		return base + "/api/v1/userstories?page=1"
	})
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.GetAll(context.Background(), "userstories", nil)
	if err == nil || len(*pages) != 1 {
		t.Fatalf("err=%v pages=%v", err, *pages)
	}
}
