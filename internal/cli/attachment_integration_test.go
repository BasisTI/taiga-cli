//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func attachmentList(t *testing.T, env map[string]string, args ...string) []map[string]any {
	t.Helper()
	out, errOut, code := runIn(t, env, "", append([]string{"attachment", "list"}, args...)...)
	if code != 0 {
		t.Fatalf("attachment list %v: %d %s", args, code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func localFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestIntegrationAttachmentsStoryAndTask uploads, lists and downloads through the gateway of
// compose.test.yml, which serves the signed media URLs like a production deployment. svc, a
// plain member, does the work in a project of its own.
func TestIntegrationAttachmentsStoryAndTask(t *testing.T) {
	env, svc, pid := freshProject(t, "cli-test-attachments-"+fmt.Sprint(time.Now().UnixNano()))
	story := storyJSON(t, env, "", "story", "create", "--subject", "with attachments")
	ref := fmt.Sprint(story["ref"])
	task := storyJSON(t, env, "", "api", "POST", "tasks", "-F", "project="+pid, "-F", fmt.Sprintf("user_story=%v", story["id"]), "-f", "subject=task with attachments")
	taskRef := fmt.Sprint(task["ref"])

	content := []byte("taiga-cli attachment\x00\xff\nsecond line\n")
	file := localFile(t, "relatório final.txt", content)
	up := storyJSON(t, svc, "", "attachment", "upload", ref, file, "--description", "weekly report")
	if up["created"] != true || up["name"] != "relatório final.txt" || up["size"] != float64(len(content)) || up["description"] != "weekly report" || up["url"] != nil {
		t.Fatalf("upload: %v", up)
	}
	// The same file again is a no-op; the same content under another name is a new attachment.
	if again := storyJSON(t, svc, "", "attachment", "upload", ref, file); again["created"] != false || again["id"] != up["id"] {
		t.Fatalf("repeat: %v", again)
	}
	if renamed := storyJSON(t, svc, "", "attachment", "upload", ref, localFile(t, "copy.txt", content)); renamed["created"] != true || renamed["id"] == up["id"] {
		t.Fatalf("renamed: %v", renamed)
	}
	taskUp := storyJSON(t, svc, "", "attachment", "upload", taskRef, localFile(t, "task.txt", []byte("task bytes")), "--task")
	if taskUp["object_id"] != task["id"] {
		t.Fatalf("task upload: %v", taskUp)
	}

	items := attachmentList(t, svc, ref)
	if len(items) != 2 || items[0]["id"] != up["id"] {
		t.Fatalf("story list: %v", items)
	}
	for _, it := range items {
		if _, ok := it["url"]; ok {
			t.Fatalf("list shows the signed url: %v", it)
		}
	}
	if taskItems := attachmentList(t, svc, taskRef, "--task"); len(taskItems) != 1 || taskItems[0]["id"] != taskUp["id"] {
		t.Fatalf("task list: %v", taskItems)
	}

	id := fmt.Sprint(up["id"])
	dir := t.TempDir()
	got := storyJSON(t, svc, "", "attachment", "download", ref, id, "--to", dir)
	saved := filepath.Join(dir, "relatório final.txt")
	if b, err := os.ReadFile(saved); err != nil || !bytes.Equal(b, content) || got["path"] != saved {
		t.Fatalf("download: %v %v %q", got, err, b)
	}
	if _, errOut, code := runIn(t, svc, "", "attachment", "download", ref, id, "--to", dir); code != 2 || !strings.Contains(errOut, "--overwrite") {
		t.Fatalf("existing: %d %s", code, errOut)
	}
	if err := os.WriteFile(saved, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	storyJSON(t, svc, "", "attachment", "download", ref, id, "--to", dir, "--overwrite")
	if b, _ := os.ReadFile(saved); !bytes.Equal(b, content) {
		t.Fatalf("overwrite: %q", b)
	}
	out, errOut, code := runIn(t, svc, "", "attachment", "download", taskRef, fmt.Sprint(taskUp["id"]), "--task", "--to", "-")
	if code != 0 || out != "task bytes" {
		t.Fatalf("stdout: %d %q %s", code, out, errOut)
	}
	// A story attachment asked through the task: not one of its attachments.
	if _, errOut, code := runIn(t, svc, "", "attachment", "download", taskRef, id, "--task", "--to", "-"); code != 5 {
		t.Fatalf("cross kind: %d %s", code, errOut)
	}
}

// The gateway of compose.test.yml refuses bodies above 50 MB, like the Basis proxy.
func TestIntegrationUploadTooLarge(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-attachments-large-"+fmt.Sprint(time.Now().UnixNano()))
	story := storyJSON(t, env, "", "story", "create", "--subject", "large attachment")
	file := localFile(t, "large.bin", bytes.Repeat([]byte{'x'}, 50<<20+1))
	_, errOut, code := runIn(t, env, "", "attachment", "upload", fmt.Sprint(story["ref"]), file)
	if code != 2 || !strings.Contains(errOut, "payload_too_large") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// A 201 whose body is cut: the list shows the new attachment, so the upload is a success.
func TestIntegrationUploadTruncatedAnswer(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-attachments-cut-"+fmt.Sprint(time.Now().UnixNano()))
	story := storyJSON(t, env, "", "story", "create", "--subject", "cut answer")
	posts := 0
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/userstories/attachments") {
			return false
		}
		posts++
		resp := forward(t, r, body)
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(resp.status)
		_, _ = w.Write(resp.body[:10])
		return true
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	got := storyJSON(t, proxied, "", "attachment", "upload", fmt.Sprint(story["ref"]), localFile(t, "cut.txt", []byte("cut")))
	if got["created"] != true || got["name"] != "cut.txt" || posts != 1 {
		t.Fatalf("%v posts=%d", got, posts)
	}
}

// The connection drops after the POST reached Taiga: the list shows it, so it is a success; an
// answer lost before anything was stored is attachment_unconfirmed, never a repeat.
func TestIntegrationUploadLostAnswer(t *testing.T) {
	env, _, _ := freshProject(t, "cli-test-attachments-lost-"+fmt.Sprint(time.Now().UnixNano()))
	story := storyJSON(t, env, "", "story", "create", "--subject", "lost answer")
	ref := fmt.Sprint(story["ref"])
	posts, forwardPost := 0, true
	url := proxy(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/userstories/attachments") {
			return false
		}
		posts++
		if forwardPost {
			forward(t, r, body)
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return true
	})
	proxied := map[string]string{"TAIGA_URL": url, "TAIGA_TOKEN": env["TAIGA_TOKEN"], "TAIGA_PROJECT": env["TAIGA_PROJECT"]}
	got := storyJSON(t, proxied, "", "attachment", "upload", ref, localFile(t, "lost.txt", []byte("lost")))
	if got["created"] != true || posts != 1 {
		t.Fatalf("%v posts=%d", got, posts)
	}
	forwardPost = false
	_, errOut, code := runIn(t, proxied, "", "attachment", "upload", ref, localFile(t, "never.txt", []byte("never")))
	if code != 1 || !strings.Contains(errOut, "attachment_unconfirmed") || posts != 2 {
		t.Fatalf("%d %s posts=%d", code, errOut, posts)
	}
	if items := attachmentList(t, env, ref); len(items) != 1 {
		t.Fatalf("list: %v", items)
	}
}

type forwarded struct {
	status int
	body   []byte
}

// forward sends the request to the local Taiga and returns its answer.
func forward(t *testing.T, r *http.Request, body []byte) forwarded {
	t.Helper()
	req, _ := http.NewRequest(r.Method, testtaiga.URL()+r.URL.RequestURI(), bytes.NewReader(body))
	req.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("forward: %v", err)
		return forwarded{status: 502}
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return forwarded{resp.StatusCode, buf.Bytes()}
}
