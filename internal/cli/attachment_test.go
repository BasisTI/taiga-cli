package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// attachmentFake is a Taiga with story 246 (id 6808) and task 250 (id 9100) in project 37,
// their attachments, and the media URLs that serve them.
type attachmentFake struct {
	t       *testing.T
	mu      sync.Mutex
	srv     string
	nextID  int
	stored  map[string][]map[string]any // by endpoint
	content map[int]string
	posts   int
}

func newAttachmentFake(t *testing.T) (*attachmentFake, *[]recorded) {
	f := &attachmentFake{t: t, nextID: 100, stored: map[string][]map[string]any{}, content: map[int]string{}}
	srv, calls := fakeTaiga(t, f.handle)
	f.srv = srv.URL
	return f, calls
}

func (f *attachmentFake) env() map[string]string {
	return map[string]string{"TAIGA_URL": f.srv, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra"}
}

func hexSum(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func (f *attachmentFake) add(endpoint string, objectID int, name, content string) map[string]any {
	f.nextID++
	kind := map[string]string{"userstories/attachments": "userstory", "tasks/attachments": "task"}[endpoint]
	o := map[string]any{"id": f.nextID, "object_id": objectID, "project": 37, "name": name, "size": len(content), "sha1": hexSum(content),
		"description": "", "is_deprecated": false, "owner": 5, "created_date": "2026-10-02T10:00:00Z",
		"url": fmt.Sprintf("%s/media/attachments/%d/f?token=secret-token#_taiga-refresh=%s:%d", f.srv, f.nextID, kind, f.nextID)}
	f.stored[endpoint] = append(f.stored[endpoint], o)
	f.content[f.nextID] = content
	return o
}

func (f *attachmentFake) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case path == "projects/by_slug":
		write(map[string]any{"id": 37, "slug": "infra"})
	case path == "userstories/by_ref":
		write(map[string]any{"id": 6808, "ref": 246, "project": 37, "version": 3})
	case path == "tasks/by_ref":
		write(map[string]any{"id": 9100, "ref": 250, "project": 37, "version": 2})
	case (path == "userstories/attachments" || path == "tasks/attachments") && r.Method == "GET":
		out := []map[string]any{}
		for _, o := range f.stored[path] {
			if fmt.Sprint(o["object_id"]) == r.URL.Query().Get("object_id") {
				out = append(out, o)
			}
		}
		write(out)
	case (path == "userstories/attachments" || path == "tasks/attachments") && r.Method == "POST":
		f.posts++
		file, h, err := r.FormFile("attached_file")
		if err != nil {
			f.t.Errorf("multipart: %v", err)
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(file)
		var objectID int
		_, _ = fmt.Sscan(r.FormValue("object_id"), &objectID)
		o := f.add(path, objectID, h.Filename, string(b))
		o["description"] = r.FormValue("description")
		w.WriteHeader(201)
		write(o)
	case strings.HasPrefix(r.URL.Path, "/media/attachments/"):
		var id int
		_, _ = fmt.Sscanf(r.URL.Path, "/media/attachments/%d/f", &id)
		if r.URL.Query().Get("token") != "secret-token" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(403)
			return
		}
		_, _ = io.WriteString(w, f.content[id])
	default:
		for endpoint, list := range f.stored {
			for _, o := range list {
				if path == fmt.Sprintf("%s/%v", endpoint, o["id"]) {
					write(o)
					return
				}
			}
		}
		w.WriteHeader(404)
	}
}

func TestAttachmentRejectsUsageBeforeNetwork(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra"}
	for _, args := range [][]string{
		{"attachment", "list"},
		{"attachment", "list", "0"},
		{"attachment", "upload", "246"},
		{"attachment", "upload", "x", "file"},
		{"attachment", "upload", "246", "file", "--timeout", "0s"},
		{"attachment", "download", "246"},
		{"attachment", "download", "246", "abc"},
		{"attachment", "download", "246", "0"},
		{"attachment", "download", "246", "7", "--to", "-", "--overwrite"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestAttachmentListHidesURLAndEscapesText(t *testing.T) {
	f, calls := newAttachmentFake(t)
	f.add("userstories/attachments", 6808, "evil\u202etxt.exe", "x")
	f.add("tasks/attachments", 6808, "task-attachment-with-same-id", "y")
	out, stderr, code := runIn(t, f.env(), "", "attachment", "list", "246")
	if code != 0 || strings.Contains(out, "secret-token") || strings.Contains(out, `"url"`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 1 || items[0]["name"] != "evil\u202etxt.exe" {
		t.Fatalf("%v %s", err, out)
	}
	text, _, code := runIn(t, f.env(), "", "attachment", "list", "246", "--output", "text")
	if code != 0 || strings.ContainsRune(text, '\u202e') || !strings.Contains(strings.Join(strings.Fields(text), " "), `name: "evil\u202etxt.exe"`) || strings.Contains(text, "secret-token") {
		t.Fatalf("%d %q", code, text)
	}
	for _, key := range []string{"id:", "size:", "sha1:", "description:", "created_date:", "owner:", "is_deprecated:"} {
		if !strings.Contains(text, key) {
			t.Fatalf("text lacks %s: %q", key, text)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("list wrote: %+v", writes(calls))
	}
}

func TestAttachmentUploadDownloadRoundTrip(t *testing.T) {
	f, _ := newAttachmentFake(t)
	file := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(file, []byte("the content"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runIn(t, f.env(), "", "attachment", "upload", "246", file, "--description", "weekly")
	var up map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &up) != nil || up["created"] != true || up["description"] != "weekly" || up["url"] != nil || f.posts != 1 {
		t.Fatalf("%d %s %s posts=%d", code, out, stderr, f.posts)
	}
	// The same file again: nothing is sent.
	out, _, code = runIn(t, f.env(), "", "attachment", "upload", "246", file)
	if code != 0 || !strings.Contains(out, `"created": false`) || f.posts != 1 {
		t.Fatalf("%d %s posts=%d", code, out, f.posts)
	}
	id := fmt.Sprint(up["id"])
	dir := t.TempDir()
	out, stderr, code = runIn(t, f.env(), "", "attachment", "download", "246", id, "--to", dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "report.txt")); code != 0 || string(b) != "the content" || strings.Contains(out, "secret-token") {
		t.Fatalf("%d %s %s %q", code, out, stderr, b)
	}
	_, stderr, code = runIn(t, f.env(), "", "attachment", "download", "246", id, "--to", dir)
	if code != 2 || !strings.Contains(stderr, "--overwrite") {
		t.Fatalf("existing: %d %s", code, stderr)
	}
	if _, stderr, code = runIn(t, f.env(), "", "attachment", "download", "246", id, "--to", dir, "--overwrite"); code != 0 {
		t.Fatalf("overwrite: %d %s", code, stderr)
	}
	out, stderr, code = runIn(t, f.env(), "", "attachment", "download", "246", id, "--to", "-")
	if code != 0 || out != "the content" {
		t.Fatalf("stdout: %d %q %s", code, out, stderr)
	}
	// The default destination is the working directory.
	var buf, errOut strings.Builder
	cwd := t.TempDir()
	env := f.env()
	env["HOME"] = t.TempDir()
	a := &App{In: strings.NewReader(""), Out: &buf, Err: &errOut, Env: func(k string) string { return env[k] }, Cwd: cwd}
	if code := a.Run([]string{"attachment", "download", "246", id}); code != 0 {
		t.Fatalf("default dest: %d %s", code, errOut.String())
	}
	if b, _ := os.ReadFile(filepath.Join(cwd, "report.txt")); string(b) != "the content" {
		t.Fatalf("default dest: %q", b)
	}
}

func TestAttachmentDryRunSendsNothing(t *testing.T) {
	f, calls := newAttachmentFake(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runIn(t, f.env(), "", "attachment", "upload", "250", file, "--task", "--dry-run")
	if code != 0 || !strings.Contains(out, `"path": "tasks/attachments"`) || !strings.Contains(out, `"object_id": "9100"`) || strings.Contains(out, "file body") || len(writes(calls)) != 0 {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}

func TestAttachmentTaskUsesTaskEndpoints(t *testing.T) {
	f, _ := newAttachmentFake(t)
	f.add("tasks/attachments", 9100, "t.txt", "task bytes")
	f.add("userstories/attachments", 6808, "s.txt", "story bytes")
	out, _, code := runIn(t, f.env(), "", "attachment", "list", "250", "--task")
	if code != 0 || !strings.Contains(out, "t.txt") || strings.Contains(out, "s.txt") {
		t.Fatalf("%d %s", code, out)
	}
	// The story attachment's id is not one of the task's attachments.
	_, stderr, code := runIn(t, f.env(), "", "attachment", "download", "250", "102", "--task", "--to", "-")
	if code != 5 || !strings.Contains(stderr, "not_found") {
		t.Fatalf("%d %s", code, stderr)
	}
}

// An interrupt (SIGINT, SIGTERM) during a download cancels it and removes the temporary file.
func TestAttachmentDownloadInterruptedLeavesNoTemp(t *testing.T) {
	f, _ := newAttachmentFake(t)
	a := f.add("userstories/attachments", 6808, "big.bin", strings.Repeat("x", 20))
	started := make(chan struct{})
	slow, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/media/") {
			f.handle(w, r)
			return
		}
		_, _ = io.WriteString(w, "xxxxxxxxxx")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	f.srv = slow.URL
	a["url"] = slow.URL + "/media/attachments/101/f?token=secret-token"
	dir := t.TempDir()
	env := f.env()
	env["HOME"] = t.TempDir()
	var out, errOut strings.Builder
	app := &App{In: strings.NewReader(""), Out: &out, Err: &errOut, Env: func(k string) string { return env[k] }, Cwd: dir}
	done := make(chan int)
	go func() { done <- app.Run([]string{"attachment", "download", "246", fmt.Sprint(a["id"]), "--to", dir}) }()
	<-started
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code == 0 {
		t.Fatalf("interrupted download succeeded: %s", out.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftovers: %v", entries)
	}
}
