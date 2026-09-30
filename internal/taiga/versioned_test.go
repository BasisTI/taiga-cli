package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeStory simulates Taiga OCC: PATCH must carry the current version.
type fakeStory struct {
	mu      sync.Mutex
	version int
	fields  map[string]any
	patches int
}

func (f *fakeStory) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case "GET":
			out := map[string]any{"version": f.version}
			for k, v := range f.fields {
				out[k] = v
			}
			_ = json.NewEncoder(w).Encode(out)
		case "PATCH":
			f.patches++
			b, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(b, &body)
			if int(body["version"].(float64)) != f.version {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"version":"The version doesn't match with the current one"}`))
				return
			}
			for k, v := range body {
				if k != "version" {
					f.fields[k] = v
				}
			}
			f.version++
			_ = json.NewEncoder(w).Encode(map[string]any{"version": f.version})
		}
	})
}

func TestWriteVersionedHappyPath(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false); err != nil {
		t.Fatal(err)
	}
	if f.fields["status"] != 2.0 || f.version != 4 || f.patches != 1 {
		t.Fatalf("%+v", f)
	}
}

func TestWriteVersionedRetriesWhenOtherFieldsChanged(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0, "subject": "a"}}
	// between our first read and our PATCH someone edits the subject (not our field)
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	origHandler := f.handler()
	var once sync.Once
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			once.Do(func() { f.mu.Lock(); f.fields["subject"] = "b"; f.version = 4; f.mu.Unlock() })
		}
		origHandler.ServeHTTP(w, r)
	})
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false); err != nil {
		t.Fatalf("should retry: %v", err)
	}
	if f.fields["status"] != 2.0 || f.fields["subject"] != "b" || f.patches != 2 {
		t.Fatalf("%+v", f)
	}
}

func TestWriteVersionedConflictsWhenOurFieldChanged(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	origHandler := f.handler()
	var once sync.Once
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			once.Do(func() { f.mu.Lock(); f.fields["status"] = 5.0; f.version = 4; f.mu.Unlock() })
		}
		origHandler.ServeHTTP(w, r)
	})
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false)
	var ce *ConflictError
	if !errors.As(err, &ce) || len(ce.Fields) != 1 || ce.Fields[0] != "status" {
		t.Fatalf("want ConflictError{status}, got %v", err)
	}
	if ToOutput(err).Exit != 4 {
		t.Fatal("exit must be 4")
	}
	// force overrides
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, true); err != nil {
		t.Fatalf("force: %v", err)
	}
}

func TestPrepareVersionedDoesNotWrite(t *testing.T) {
	f := &fakeStory{version: 9, fields: map[string]any{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	body, _, err := c.PrepareVersioned(context.Background(), "userstories/1", map[string]any{"comment": "x"})
	if err != nil || body["version"] != json.Number("9") || f.patches != 0 {
		t.Fatalf("body=%v err=%v patches=%d", body, err, f.patches)
	}
}

func TestWriteVersionedSecondConflictIsConflictError(t *testing.T) {
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"version":3,"status":1}`))
			return
		}
		atomic.AddInt32(&patches, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"version":"The version doesn't match with the current one"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false)
	var ce *ConflictError
	if !errors.As(err, &ce) || ToOutput(err).Exit != 4 || atomic.LoadInt32(&patches) != 2 {
		t.Fatalf("patches=%d err=%v", patches, err)
	}
}

func TestWriteVersionedNonConflictErrorIsReturnedWithoutReread(t *testing.T) {
	var gets, patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			atomic.AddInt32(&gets, 1)
			_, _ = w.Write([]byte(`{"version":3}`))
			return
		}
		atomic.AddInt32(&patches, 1)
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"_error_message":"no"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 403 || atomic.LoadInt32(&gets) != 1 || atomic.LoadInt32(&patches) != 1 {
		t.Fatalf("gets=%d patches=%d err=%v", gets, patches, err)
	}
}
