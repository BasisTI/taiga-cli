package taiga

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAllFollowsPagesAndSendsDisableHeader(t *testing.T) {
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("x-disable-pagination") == "True"
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("x-pagination-next", "http://x/?page=2")
			_, _ = fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			return
		}
		_, _ = fmt.Fprint(w, `[{"id":3}]`)
	}))
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
