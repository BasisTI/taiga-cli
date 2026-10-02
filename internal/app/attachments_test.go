package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// attachAPI is a Taiga with one story (6808) and one task (9100) of project 37 and their
// attachments. upload decides the answer of each POST; it may store the attachment.
type attachAPI struct {
	fakeAPI
	stored    map[string][]Object // by kind path: userstories/attachments, tasks/attachments
	nextID    int64
	uploads   int
	upload    func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error)
	listErr   error // returned by the listing after the first upload
	ctxCheck  bool  // the listing fails on a context that is done, like the real client
	content   map[int64]string
	downloads int
}

func newAttachAPI(t *testing.T) (*attachAPI, *Service) {
	t.Helper()
	f := &attachAPI{stored: map[string][]Object{}, nextID: 100, content: map[int64]string{}}
	f.objects = map[string]string{
		"userstories/by_ref": `{"id":6808,"ref":246,"project":37,"version":7}`,
		"tasks/by_ref":       `{"id":9100,"ref":250,"project":37,"version":3}`,
	}
	s := service(t, &f.fakeAPI)
	s.API = f
	return f, s
}

func sum(b string) string {
	h := sha1.Sum([]byte(b))
	return hex.EncodeToString(h[:])
}

// store saves an attachment as Taiga would and returns its JSON.
func (f *attachAPI) store(path string, objectID int64, name, content string) Object {
	f.nextID++
	o := Object{"id": json.Number(fmt.Sprint(f.nextID)), "object_id": json.Number(fmt.Sprint(objectID)), "project": json.Number("37"), "name": name,
		"size": json.Number(fmt.Sprint(len(content))), "sha1": sum(content), "description": "", "is_deprecated": false,
		"url":                fmt.Sprintf("http://taiga.test/media/attachments/x/%s?token=secret#_taiga-refresh=userstory:%d", name, f.nextID),
		"preview_url":        "http://taiga.test/media/attachments/x/p?token=secret",
		"thumbnail_card_url": "http://taiga.test/media/attachments/x/t.300x200.jpg?token=secret"}
	f.stored[path] = append(f.stored[path], o)
	f.content[f.nextID] = content
	return o
}

// accept stores the upload and answers 201 with it.
func accept(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
	id, _ := json.Marshal(a.store(path, ID(fields["object_id"]), name, string(content)))
	return &taiga.Response{Status: 201, Body: id}, nil
}

func (f *attachAPI) Upload(_ context.Context, path string, fields map[string]string, fileField, fileName string, r io.Reader, size int64) (*taiga.Response, error) {
	f.uploads++
	b, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return nil, err
	}
	if fileField != "attached_file" || int64(len(b)) != size {
		return nil, fmt.Errorf("field %q, %d bytes of %d", fileField, len(b), size)
	}
	return f.upload(f, path, fields, fileName, b)
}

func (f *attachAPI) GetAll(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	if f.ctxCheck && ctx.Err() != nil {
		return nil, &taiga.NetworkError{Method: "GET", Path: path, Err: ctx.Err()}
	}
	if f.uploads > 0 && f.listErr != nil {
		return nil, f.listErr
	}
	if q.Get("project") != "37" || q.Get("object_id") == "" {
		return nil, fmt.Errorf("query %v", q)
	}
	out := []json.RawMessage{}
	for _, o := range f.stored[path] {
		if fmt.Sprint(o["object_id"]) == q.Get("object_id") {
			b, _ := json.Marshal(o)
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *attachAPI) Do(ctx context.Context, r taiga.Request) (*taiga.Response, error) {
	for path, list := range f.stored {
		for _, o := range list {
			if r.Path == fmt.Sprintf("%s/%v", path, o["id"]) {
				b, _ := json.Marshal(o)
				return &taiga.Response{Status: 200, Body: b}, nil
			}
		}
	}
	return f.fakeAPI.Do(ctx, r)
}

func (f *attachAPI) Download(_ context.Context, rawURL string, w io.Writer) (int64, error) {
	f.downloads++
	for id, content := range f.content {
		if strings.Contains(rawURL, fmt.Sprintf("userstory:%d", id)) {
			n, err := io.WriteString(w, content)
			return int64(n), err
		}
	}
	return 0, &taiga.APIError{Status: 404, Method: "GET", Path: "/media/x"}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func story(t *testing.T, s *Service) Object {
	t.Helper()
	o, err := s.Story(context.Background(), "246", 0)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func codeOf(err error) string {
	var e *output.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return fmt.Sprintf("%T %v", err, err)
}

func TestUploadPostconditionFromAnswer(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	file := writeFile(t, "report.txt", "content")
	got, err := s.Upload(context.Background(), "story", story(t, s), file, "a note", false)
	if err != nil {
		t.Fatal(err)
	}
	o := got.(Object)
	if f.uploads != 1 || o["created"] != true || o["name"] != "report.txt" || o["sha1"] != sum("content") || o["url"] != nil {
		t.Fatalf("uploads=%d %v", f.uploads, o)
	}
}

func TestUploadSendsFieldsToTheKindPath(t *testing.T) {
	f, s := newAttachAPI(t)
	var gotPath string
	var gotFields map[string]string
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		gotPath, gotFields = path, fields
		return accept(a, path, fields, name, content)
	}
	task, err := s.Task(context.Background(), "250", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(context.Background(), "task", task, writeFile(t, "t.txt", "x"), "d", false); err != nil {
		t.Fatal(err)
	}
	if gotPath != "tasks/attachments" || gotFields["project"] != "37" || gotFields["object_id"] != "9100" || gotFields["description"] != "d" {
		t.Fatalf("%s %v", gotPath, gotFields)
	}
}

func TestUploadPostconditionMismatch(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		// The file changed between the hash and the send: the server stored other bytes.
		return accept(a, path, fields, name, []byte("other bytes"))
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	var e *output.Error
	if !errors.As(err, &e) || e.Code != "attachment_postcondition_failed" || e.Exit != output.ExitConflict ||
		!strings.Contains(e.Cause, sum("content")) || !strings.Contains(e.Cause, sum("other bytes")) || !strings.Contains(e.Cause, "101") {
		t.Fatalf("%+v", err)
	}
}

func TestUploadUnreadableAnswerUsesReread(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		a.store(path, 6808, name, string(content))
		return nil, &taiga.UnreadableBodyError{Method: "POST", Path: path, Status: 201, Err: io.ErrUnexpectedEOF}
	}
	got, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	if err != nil || got.(Object)["id"] != json.Number("101") || got.(Object)["created"] != true {
		t.Fatalf("%v %v", got, err)
	}
}

func TestUploadUnreadableAnswerAndRereadFails(t *testing.T) {
	f, s := newAttachAPI(t)
	f.listErr = &taiga.NetworkError{Method: "GET", Path: "userstories/attachments", Err: errors.New("reset")}
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		return nil, &taiga.UnreadableBodyError{Method: "POST", Path: path, Status: 201, Err: io.ErrUnexpectedEOF}
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	if codeOf(err) != "write_applied" || exitOf(err) != output.ExitUnexpected {
		t.Fatalf("%v", err)
	}
}

func TestUploadLostAnswerFoundInList(t *testing.T) {
	for _, lost := range []error{
		&taiga.NetworkError{Method: "POST", Path: "userstories/attachments", Err: &net.OpError{Op: "read", Err: errors.New("reset")}},
		&taiga.APIError{Status: 504, Method: "POST", Path: "userstories/attachments"},
	} {
		f, s := newAttachAPI(t)
		f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
			a.store(path, 6808, name, string(content))
			return nil, lost
		}
		got, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
		if err != nil || got.(Object)["id"] != json.Number("101") || f.uploads != 1 {
			t.Fatalf("%v: %v %v", lost, got, err)
		}
	}
}

func TestUploadLostAnswerNotFound(t *testing.T) {
	f, s := newAttachAPI(t)
	// An older copy with the same name and content does not count: only a new id does.
	f.store("userstories/attachments", 6808, "old.txt", "content")
	f.upload = func(*attachAPI, string, map[string]string, string, []byte) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 502, Method: "POST", Path: "userstories/attachments"}
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	var e *output.Error
	if !errors.As(err, &e) || e.Code != "attachment_unconfirmed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Recovery, "taiga attachment list") || f.uploads != 1 {
		t.Fatalf("%+v uploads=%d", err, f.uploads)
	}
}

func TestUploadNotSentIsNetworkError(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = func(*attachAPI, string, map[string]string, string, []byte) (*taiga.Response, error) {
		return nil, &taiga.NetworkError{Method: "POST", Path: "userstories/attachments", Err: &url.Error{Op: "Post", Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}}
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	if codeOf(err) != "network_error" || exitOf(err) != output.ExitNetwork {
		t.Fatalf("%v", err)
	}
}

func TestUploadRejectedIsTheAPIError(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = func(*attachAPI, string, map[string]string, string, []byte) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 413, Method: "POST", Path: "userstories/attachments", Body: []byte("too large")}
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	if codeOf(err) != "payload_too_large" || exitOf(err) != output.ExitUsage {
		t.Fatalf("%v", err)
	}
}

func TestUploadSameNameAndSha1IsNoop(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	f.store("userstories/attachments", 6808, "r.txt", "content")
	for _, dry := range []bool{true, false} {
		got, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", dry)
		o, _ := got.(Object)
		if err != nil || f.uploads != 0 || o["created"] != false || o["id"] != json.Number("101") || o["url"] != nil {
			t.Fatalf("dry=%v: %v %v uploads=%d", dry, got, err, f.uploads)
		}
	}
	// Same content under another name, or another content under the same name: sent.
	for _, tc := range [][2]string{{"other.txt", "content"}, {"r.txt", "changed"}} {
		if _, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, tc[0], tc[1]), "", false); err != nil {
			t.Fatal(err)
		}
	}
	if f.uploads != 2 {
		t.Fatalf("uploads=%d", f.uploads)
	}
}

func TestUploadDryRunShowsFileMetadataOnly(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	got, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "secret file body"), "note", true)
	if err != nil || f.uploads != 0 {
		t.Fatalf("%v uploads=%d", err, f.uploads)
	}
	b, _ := json.Marshal(got)
	var plan struct {
		DryRun bool              `json:"dry_run"`
		Method string            `json:"method"`
		Path   string            `json:"path"`
		Fields map[string]string `json:"fields"`
		File   map[string]any    `json:"file"`
	}
	if err := json.Unmarshal(b, &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.DryRun || plan.Method != "POST" || plan.Path != "userstories/attachments" || plan.Fields["object_id"] != "6808" || plan.Fields["project"] != "37" ||
		plan.Fields["description"] != "note" || plan.File["name"] != "r.txt" || plan.File["size"] != float64(16) || plan.File["sha1"] != sum("secret file body") ||
		strings.Contains(string(b), "secret file body") {
		t.Fatalf("%s", b)
	}
}

func TestUploadRefusesDirectory(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, broken, filepath.Join(dir, "absent"), writeFile(t, "empty.txt", ""), os.DevNull} {
		_, err := s.Upload(context.Background(), "story", story(t, s), p, "", false)
		if codeOf(err) != "usage" {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if f.uploads != 0 {
		t.Fatalf("uploads=%d", f.uploads)
	}
}

func TestAttachmentsHideURLAndKeepOwnerOnly(t *testing.T) {
	f, s := newAttachAPI(t)
	f.store("userstories/attachments", 6808, "a.txt", "a")
	f.store("userstories/attachments", 6900, "other.txt", "b")
	f.store("tasks/attachments", 6808, "task-with-same-id.txt", "c")
	got, err := s.Attachments(context.Background(), "story", story(t, s))
	if err != nil || len(got) != 1 || got[0]["name"] != "a.txt" || got[0]["url"] != nil {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := s.Attachments(context.Background(), "epic", story(t, s)); codeOf(err) != "usage" {
		t.Fatalf("kind: %v", err)
	}
}

func TestDownloadSanitizesServerName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"report.txt", "report.txt"},
		{"../../etc/passwd", "passwd"},
		{"a/b", "b"},
		{`a\b`, "b"},
		{"\x1b]0;x\a", "_]0;x_"},
		{"evil\u202etxt.exe", "evil_txt.exe"},
		{".bash_profile", "_bash_profile"},
		{"..hidden", "_.hidden"},
		{"nul\x00byte", "nul_byte"},
		{".", "attachment-7"},
		{"..", "attachment-7"},
		{"", "attachment-7"},
		{"dir/", "attachment-7"},
	} {
		if got := safeName(tc.name, 7); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDownloadWritesTheCheckedFile(t *testing.T) {
	f, s := newAttachAPI(t)
	a := f.store("userstories/attachments", 6808, "../report.txt", "content")
	dir := t.TempDir()
	got, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), dir, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "report.txt")
	b, rerr := os.ReadFile(dest)
	info, _ := os.Stat(dest)
	if rerr != nil || string(b) != "content" || got["path"] != dest || got["url"] != nil || info.Mode().Perm() != 0o666&^currentUmask() {
		t.Fatalf("%v %q %v", got, b, rerr)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
	// An explicit file path, then --overwrite on it.
	file := filepath.Join(dir, "chosen.bin")
	for _, overwrite := range []bool{false, true} {
		if _, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), file, overwrite, nil); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(file); string(b) != "content" {
		t.Fatalf("%q", b)
	}
}

func TestDownloadToWriter(t *testing.T) {
	f, s := newAttachAPI(t)
	a := f.store("userstories/attachments", 6808, "r.txt", "content")
	var buf bytes.Buffer
	if _, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), "-", false, &buf); err != nil || buf.String() != "content" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	f.content[ID(a["id"])] = "changed"
	buf.Reset()
	_, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), "-", false, &buf)
	if codeOf(err) != "attachment_download_mismatch" || exitOf(err) != output.ExitNetwork {
		t.Fatalf("%v", err)
	}
}

func TestDownloadRefusesExistingWithoutOverwrite(t *testing.T) {
	f, s := newAttachAPI(t)
	a := f.store("userstories/attachments", 6808, "r.txt", "content")
	dir := t.TempDir()
	existing := writeFile(t, "x", "mine")
	if err := os.WriteFile(filepath.Join(dir, "r.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{dir, existing} {
		_, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), dest, false, nil)
		if codeOf(err) != "usage" || f.downloads != 0 {
			t.Fatalf("%s: %v downloads=%d", dest, err, f.downloads)
		}
	}
	if b, _ := os.ReadFile(existing); string(b) != "mine" {
		t.Fatal("overwritten")
	}
}

func TestDownloadMismatchRemovesTemp(t *testing.T) {
	f, s := newAttachAPI(t)
	a := f.store("userstories/attachments", 6808, "r.txt", "content")
	f.content[ID(a["id"])] = "tampered"
	dir := t.TempDir()
	_, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), dir, false, nil)
	var e *output.Error
	if !errors.As(err, &e) || e.Code != "attachment_download_mismatch" || e.Exit != output.ExitNetwork {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestDownloadChecksObjectID(t *testing.T) {
	f, s := newAttachAPI(t)
	// Taiga reads any attachment through any kind (docs/api-notes.md): an attachment of another
	// story, or of a task with the same id, is not this story's.
	other := f.store("userstories/attachments", 6900, "o.txt", "o")
	sameID := f.store("tasks/attachments", 6808, "t.txt", "t")
	for _, id := range []int64{ID(other["id"]), ID(sameID["id"]), 999} {
		_, err := s.DownloadAttachment(context.Background(), "story", story(t, s), id, t.TempDir(), false, nil)
		if codeOf(err) != "not_found" || exitOf(err) != output.ExitNotFound || f.downloads != 0 {
			t.Fatalf("%d: %v", id, err)
		}
	}
}

func TestTaskByRefChecksProject(t *testing.T) {
	f, s := newAttachAPI(t)
	if task, err := s.Task(context.Background(), "250", 0); err != nil || ID(task["id"]) != 9100 {
		t.Fatalf("%v %v", task, err)
	}
	f.objects["tasks/by_ref"] = `{"id":9100,"ref":250,"project":38,"version":3}`
	if _, err := s.Task(context.Background(), "250", 0); codeOf(err) != "usage" {
		t.Fatalf("other project: %v", err)
	}
	f.objects["tasks/by_ref"] = `{"id":9100,"ref":251,"project":37,"version":3}`
	if _, err := s.Task(context.Background(), "250", 0); err == nil {
		t.Fatal("another ref accepted")
	}
	if _, err := s.Task(context.Background(), "0", 0); codeOf(err) != "usage" {
		t.Fatalf("ref 0: %v", err)
	}
}

// No signed link reaches the output: url, preview_url, thumbnail_card_url or any other value
// that carries a token.
func TestAttachmentViewDropsEverySignedURL(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	a := f.store("userstories/attachments", 6808, "a.txt", "a")
	a["future_url"] = "http://taiga.test/media/x?token=secret"
	list, err := s.Attachments(context.Background(), "story", story(t, s))
	if err != nil {
		t.Fatal(err)
	}
	up, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "b.txt", "b"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), "-", false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{list, up, got} {
		if b, _ := json.Marshal(v); strings.Contains(string(b), "token=") || strings.Contains(string(b), "secret") {
			t.Fatalf("signed url in output: %s", b)
		}
	}
}

// Names Taiga does not store as sent would break the idempotence by name and sha1: refused.
func TestUploadRefusesNamesTaigaRewrites(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = accept
	for _, name := range []string{"nl\r\nX.txt", `back\slash.txt`, "a&amp;b.txt", "a&#34;b.txt", "tab\tname.txt", "report&amp.txt", "report&#65.txt", "report&#x41.txt", "x&copy.txt"} {
		_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, name, "x"), "", false)
		if codeOf(err) != "usage" {
			t.Fatalf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"a & b.txt", "relatório \"final\".txt", "50%.txt"} {
		if _, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, name, name), "", false); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if f.uploads != 3 {
		t.Fatalf("uploads=%d", f.uploads)
	}
}

// The upload outlived its deadline: the check of the list still runs, on a context of its own.
func TestUploadLostAnswerAfterDeadlineIsChecked(t *testing.T) {
	f, s := newAttachAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	owner := story(t, s)
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		a.store(path, 6808, name, string(content))
		cancel()
		return nil, &taiga.NetworkError{Method: "POST", Path: path, Err: context.Canceled}
	}
	f.ctxCheck = true
	got, err := s.Upload(ctx, "story", owner, writeFile(t, "r.txt", "content"), "", false)
	if err != nil || got.(Object)["created"] != true {
		t.Fatalf("%v %v", got, err)
	}
}

// The answer must carry the name sent: a name Taiga rewrote breaks the idempotence.
func TestUploadPostconditionChecksName(t *testing.T) {
	f, s := newAttachAPI(t)
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		return accept(a, path, fields, "renamed.txt", content)
	}
	_, err := s.Upload(context.Background(), "story", story(t, s), writeFile(t, "r.txt", "content"), "", false)
	var e *output.Error
	if !errors.As(err, &e) || e.Code != "attachment_postcondition_failed" || !strings.Contains(e.Cause, "renamed.txt") {
		t.Fatalf("%v", err)
	}
}

// Every string of the dry-run plan is redacted, the file name included; a real upload still
// sends the name as it is (round 2 of the PR #12 review).
func TestUploadDryRunRedactsEveryField(t *testing.T) {
	f, s := newAttachAPI(t)
	var sent string
	f.upload = func(a *attachAPI, path string, fields map[string]string, name string, content []byte) (*taiga.Response, error) {
		sent = name
		return accept(a, path, fields, name, content)
	}
	file := writeFile(t, "report.txt?token=SIGNED_SENTINEL", "content")
	plan, err := s.Upload(context.Background(), "story", story(t, s), file, "see http://h/m?token=OTHER_SENTINEL", true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(plan)
	if strings.Contains(string(b), "SENTINEL") || !strings.Contains(string(b), `"name":"report.txt?…"`) || !strings.Contains(string(b), `"sha1":"`+sum("content")+`"`) {
		t.Fatalf("%s", b)
	}
	if _, err := s.Upload(context.Background(), "story", story(t, s), file, "", false); err != nil || sent != "report.txt?token=SIGNED_SENTINEL" {
		t.Fatalf("sent %q: %v", sent, err)
	}
}

// Failures of the local file system around the download are local_write_failed, not unexpected.
func TestDownloadLocalFailuresAreLocalWriteFailed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes in read-only directories")
	}
	f, s := newAttachAPI(t)
	a := f.store("userstories/attachments", 6808, "r.txt", "content")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	_, err := s.DownloadAttachment(context.Background(), "story", story(t, s), ID(a["id"]), dir, false, nil)
	var e *output.Error
	if !errors.As(err, &e) || e.Code != "local_write_failed" || e.Source != "file" || e.Exit != output.ExitUnexpected {
		t.Fatalf("%+v", err)
	}
}
