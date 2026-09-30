package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteVersionedFromPreservesMergeBaseline(t *testing.T) {
	for _, field := range []string{"tags", "assigned_users", "attributes_values", "description"} {
		t.Run(field, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": 4, field: "concurrent"})
					return
				}
				writes++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["version"] != float64(3) {
					t.Errorf("version=%v", body["version"])
				}
				w.WriteHeader(409)
				_, _ = fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			c := New(srv.URL, StaticToken{}, WithRetryWait(0))
			first := map[string]json.RawMessage{"version": json.RawMessage(`3`), field: json.RawMessage(`"old"`)}
			_, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1",
				map[string]any{field: "merged"}, first, false)
			var conflict *ConflictError
			if !errors.As(err, &conflict) || writes != 1 {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestWriteVersionedFromRetriesOnlyUnchangedFields(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = fmt.Fprint(w, `{"version":4,"status":1,"subject":"new"}`)
			return
		}
		writes++
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		want := float64(3)
		if writes == 2 {
			want = 4
		}
		if b["version"] != want {
			t.Errorf("body=%v", b)
		}
		if writes == 1 {
			w.WriteHeader(409)
			return
		}
		_, _ = fmt.Fprint(w, `{"version":5}`)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2},
		map[string]json.RawMessage{"version": json.RawMessage(`3`), "status": json.RawMessage(`1`)}, false)
	if err != nil || writes != 2 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}

func TestWriteVersionedFromForceRetriesChangedField(t *testing.T) {
	f := &fakeStory{version: 4, fields: map[string]any{"tags": "concurrent"}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	first := map[string]json.RawMessage{"version": json.RawMessage(`3`), "tags": json.RawMessage(`"old"`)}
	if _, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"tags": "merged"}, first, true); err != nil {
		t.Fatal(err)
	}
	if f.patches != 2 || f.fields["tags"] != "merged" || f.version != 5 {
		t.Fatalf("%+v", f)
	}
}

func TestWriteVersionedFromSecondConflictExitsConflict(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = fmt.Fprint(w, `{"version":4,"status":1}`)
			return
		}
		writes++
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, `{"version":"The version doesn't match with the current one"}`)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2},
		map[string]json.RawMessage{"version": json.RawMessage(`3`), "status": json.RawMessage(`1`)}, true)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || writes != 2 || ToOutput(err).Exit != 4 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}

func TestWriteVersionedFromRejectsMissingOrInvalidVersion(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	for _, first := range []map[string]json.RawMessage{
		{"status": json.RawMessage(`1`)},
		{"version": json.RawMessage(`null`)},
		{"version": json.RawMessage(`"3"`)},
		{"version": json.RawMessage(`-1`)},
	} {
		if _, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, first, false); err == nil {
			t.Fatalf("accepted %v", first)
		}
	}
	if requests != 0 {
		t.Fatalf("sent %d requests without a valid version", requests)
	}
}

func TestWriteVersionedFromDoesNotRepeatPatchOnServerError(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2},
		map[string]json.RawMessage{"version": json.RawMessage(`3`)}, true)
	if err == nil || writes != 1 || ToOutput(err).Exit != 7 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}

// 412 Precondition Failed with a version key is a version conflict too: re-read, then retry
// only when the patched fields did not change (exit 4 otherwise).
func TestWriteVersionedFromTreats412AsVersionConflict(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reread     string
		wantErr    bool
		wantWrites int
	}{
		{"unchanged field retries", `{"version":4,"status":1}`, false, 2},
		{"changed field conflicts", `{"version":4,"status":9}`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					_, _ = fmt.Fprint(w, tc.reread)
					return
				}
				writes++
				if writes == 1 {
					w.WriteHeader(412)
					_, _ = fmt.Fprint(w, `{"version":"The version does not match"}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"version":5}`)
			}))
			defer srv.Close()
			c := New(srv.URL, StaticToken{}, WithRetryWait(0))
			_, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2},
				map[string]json.RawMessage{"version": json.RawMessage(`3`), "status": json.RawMessage(`1`)}, false)
			if (err != nil) != tc.wantErr || writes != tc.wantWrites {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
			if tc.wantErr && ToOutput(err).Exit != 4 {
				t.Fatalf("exit %d", ToOutput(err).Exit)
			}
		})
	}
	if ToOutput(&APIError{Status: 412, Method: "PATCH", Path: "x", Body: []byte(`{"version":"x"}`)}).Code != "version_conflict" {
		t.Fatal("412 not mapped to version_conflict")
	}
}
