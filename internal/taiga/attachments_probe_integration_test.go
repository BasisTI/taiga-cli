//go:build integration

package taiga

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// probeUpload sends one multipart POST to <kind>/attachments, built in the test so the probe
// does not depend on the client code it validates. It returns the status and the decoded body.
func probeUpload(t *testing.T, token string, kind string, project, objectID int64, name string, content []byte, description string) (int, probeObject) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"project": fmt.Sprint(project), "object_id": fmt.Sprint(objectID), "description": description} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("attached_file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", testtaiga.URL()+"/api/v1/"+kind+"/attachments", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var o probeObject
	_ = json.Unmarshal(body, &o)
	if resp.StatusCode != 201 {
		t.Logf("upload %s %q: HTTP %d %s", kind, name, resp.StatusCode, body)
	}
	return resp.StatusCode, o
}

func (o probeObject) str(key string) string {
	var s string
	_ = json.Unmarshal(o[key], &s)
	return s
}

func sha1Hex(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// probeGet fetches rawURL with an optional Authorization header, without following redirects.
func probeGet(t *testing.T, rawURL, auth string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	hc := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := hc.Do(req)
	if err != nil {
		t.Logf("GET %s: %v", rawURL, err)
		return 0, nil, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, resp.Header, body
}

// TestProbeAttachmentContract records the attachment contract of Taiga 6.7 that US #251 relies
// on: the upload answer, the names the storage keeps, empty files, the list filters, foreign
// objects, duplicates, permissions, the story version and the download path. It uses projects
// of its own.
func TestProbeAttachmentContract(t *testing.T) {
	c := probeClient(t)
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	suffix := fmt.Sprint(time.Now().UnixNano())
	project := ensureProject(t, c, "cli-test-probe-attachments-"+suffix)
	other := ensureProject(t, c, "cli-test-probe-attachments-other-"+suffix)
	story := createStory(t, c, project, "attach "+suffix, nil)
	task := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "subject": "attach task " + suffix, "user_story": story.int("id")})
	storyPath := fmt.Sprintf("userstories/%d", story.int("id"))
	versionBefore := probeDo(t, c, "GET", storyPath, nil, nil).int("version")

	// 1. The 201 answer: fields, sha1 and size computed by the server.
	content := []byte("taiga-cli attachment probe " + suffix + "\n")
	status, a := probeUpload(t, token, "userstories", project, story.int("id"), "probe.txt", content, "first")
	if status != 201 {
		t.Fatalf("upload: %d", status)
	}
	for _, k := range []string{"id", "name", "size", "sha1", "url", "object_id", "project", "order", "is_deprecated", "from_comment", "description", "created_date", "owner"} {
		if _, ok := a[k]; !ok {
			t.Errorf("attachment has no %q", k)
		}
	}
	if _, ok := a["version"]; ok {
		t.Errorf("attachment has a version: %s", a["version"])
	}
	if a.str("sha1") != sha1Hex(content) || a.int("size") != int64(len(content)) {
		t.Fatalf("sha1/size: got %s/%d want %s/%d", a.str("sha1"), a.int("size"), sha1Hex(content), len(content))
	}
	if a.int("object_id") != story.int("id") || a.int("project") != project || a.str("name") != "probe.txt" || a.str("description") != "first" {
		t.Fatalf("answer: %v", a)
	}
	attURL := a.str("url")
	t.Logf("FINDING upload answer: id=%d name=%q size=%d order=%s is_deprecated=%s from_comment=%s owner=%s url=%q",
		a.int("id"), a.str("name"), a.int("size"), a["order"], a["is_deprecated"], a["from_comment"], a["owner"], attURL)

	// 8. The upload writes history on the story: does its version move?
	versionAfter := probeDo(t, c, "GET", storyPath, nil, nil).int("version")
	t.Logf("FINDING story version before/after upload: %d → %d", versionBefore, versionAfter)

	// 2. Names: accents, spaces, quotes and a path component.
	for _, name := range []string{"relatório final.txt", `with "quotes".txt`, "../x", "dir/sub/inner.txt"} {
		status, o := probeUpload(t, token, "userstories", project, story.int("id"), name, []byte("n"), "")
		t.Logf("FINDING upload name %q: HTTP %d name=%q url=%q", name, status, o.str("name"), o.str("url"))
	}

	// 3. An empty file.
	status, empty := probeUpload(t, token, "userstories", project, story.int("id"), "empty.txt", nil, "")
	t.Logf("FINDING empty file: HTTP %d size=%s sha1=%q", status, empty["size"], empty.str("sha1"))

	// 4. The list filters: only the attachments of the object, with and without pagination.
	_, ta := probeUpload(t, token, "tasks", project, task.int("id"), "task.txt", []byte("task"), "")
	if ta.int("object_id") != task.int("id") {
		t.Fatalf("task upload: %v", ta)
	}
	storyList := probeList(t, c, "userstories/attachments", url.Values{"project": {fmt.Sprint(project)}, "object_id": {fmt.Sprint(story.int("id"))}})
	for _, o := range storyList {
		if o.int("object_id") != story.int("id") {
			t.Fatalf("story list has object %d", o.int("object_id"))
		}
	}
	taskList := probeList(t, c, "tasks/attachments", url.Values{"project": {fmt.Sprint(project)}, "object_id": {fmt.Sprint(task.int("id"))}})
	if len(taskList) != 1 || taskList[0].int("id") != ta.int("id") {
		t.Fatalf("task list: %v", taskList)
	}
	// Without object_id the list carries every attachment of that kind in the project.
	all := probeList(t, c, "userstories/attachments", url.Values{"project": {fmt.Sprint(project)}})
	resp, err := c.Do(t.Context(), Request{Method: "GET", Path: "userstories/attachments", Query: url.Values{"project": {fmt.Sprint(project)}, "object_id": {fmt.Sprint(story.int("id"))}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINDING list: story=%d task=%d all-story-kind=%d; pagination headers: x-pagination-count=%q x-paginated=%q",
		len(storyList), len(taskList), len(all), resp.Header.Get("X-Pagination-Count"), resp.Header.Get("X-Paginated"))
	var order []int64
	for _, o := range storyList {
		order = append(order, o.int("id"))
	}
	t.Logf("FINDING list order (ids): %v", order)

	// 5. object_id of another project's story with this project: refused.
	foreign := createStory(t, c, other, "foreign "+suffix, nil)
	status, body := probeUpload(t, token, "userstories", project, foreign.int("id"), "foreign.txt", []byte("f"), "")
	t.Logf("FINDING foreign object_id: HTTP %d %v", status, body)
	if status != 400 {
		t.Errorf("foreign object_id: %d", status)
	}
	// A task id given to the story endpoint (same project): what happens?
	status, body = probeUpload(t, token, "userstories", project, task.int("id")+100000, "missing.txt", []byte("m"), "")
	t.Logf("FINDING missing object_id: HTTP %d %v", status, body)

	// 6. The same file twice: two attachments.
	_, dup := probeUpload(t, token, "userstories", project, story.int("id"), "probe.txt", content, "first")
	if dup.int("id") == a.int("id") || dup.str("sha1") != a.str("sha1") {
		t.Fatalf("duplicate upload: %v", dup)
	}
	t.Logf("FINDING duplicate upload: accepted as a new attachment %d (first %d)", dup.int("id"), a.int("id"))

	// GET of one attachment: fresh url (new token?) and object_id.
	one := probeDo(t, c, "GET", fmt.Sprintf("userstories/attachments/%d", a.int("id")), nil, nil)
	t.Logf("FINDING GET one: object_id=%d url=%q (upload url %q)", one.int("object_id"), one.str("url"), attURL)
	// The detail endpoint does not check the kind: a story attachment is read through tasks/.
	cross := probeDo(t, c, "GET", fmt.Sprintf("tasks/attachments/%d", a.int("id")), nil, nil)
	t.Logf("FINDING story attachment %d read through tasks/attachments: HTTP 200 object_id=%d (the story id)", a.int("id"), cross.int("object_id"))

	// 7. svc as a member uploads and lists; svc outside a private project does not.
	svcID := userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svcID, testtaiga.ServiceEmail)
	svcToken, _ := testtaiga.Login(t, testtaiga.ServiceUser, testtaiga.ServicePassword)
	svc := svcClient(t)
	status, sa := probeUpload(t, svcToken, "userstories", project, story.int("id"), "svc.txt", []byte("svc"), "")
	if status != 201 || sa.int("owner") != svcID {
		t.Fatalf("svc upload: %d %v", status, sa)
	}
	if got := probeList(t, svc, "userstories/attachments", url.Values{"project": {fmt.Sprint(project)}, "object_id": {fmt.Sprint(story.int("id"))}}); len(got) == 0 {
		t.Fatal("svc list empty")
	}
	// The other project is private and svc is not a member there.
	op := probeDo(t, c, "GET", fmt.Sprintf("projects/%d", other), nil, nil)
	_, fa := probeUpload(t, token, "userstories", other, foreign.int("id"), "private.txt", []byte("p"), "")
	status, _ = probeUpload(t, svcToken, "userstories", other, foreign.int("id"), "outsider.txt", []byte("o"), "")
	outsiderList := probeList(t, svc, "userstories/attachments", url.Values{"project": {fmt.Sprint(other)}, "object_id": {fmt.Sprint(foreign.int("id"))}})
	_, gerr := svc.Do(t.Context(), Request{Method: "GET", Path: fmt.Sprintf("userstories/attachments/%d", fa.int("id"))})
	if len(outsiderList) != 0 || probeStatus(gerr) == 0 {
		t.Fatalf("outsider reads the private project: list=%d get=%v", len(outsiderList), gerr)
	}
	t.Logf("FINDING outsider (other project is_private=%s): upload HTTP %d, list HTTP 200 with %d items, GET attachment HTTP %d",
		op["is_private"], status, len(outsiderList), probeStatus(gerr))

	// Download gate: does the taiga-back of compose.test.yml serve the url it returns?
	probeDownload(t, attURL, one.str("url"), token, content)
	t.Log("FINDING attachments: see the lines above; summary in docs/api-notes.md")
}

// probeDownload records what the url of an attachment answers: with its token, without it, with
// a tampered token, and with and without Authorization.
func probeDownload(t *testing.T, uploadURL, freshURL, token string, content []byte) {
	t.Helper()
	u, err := url.Parse(freshURL)
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	base, _ := url.Parse(testtaiga.URL())
	t.Logf("FINDING download url: scheme=%s host=%s path=%s query-keys=%v fragment=%q same-origin-as-TAIGA_TEST_URL=%v",
		u.Scheme, u.Host, u.Path, keys(u.Query()), u.Fragment, u.Scheme == base.Scheme && u.Host == base.Host)
	noFrag := *u
	noFrag.Fragment = ""
	noToken := noFrag
	noToken.RawQuery = ""
	tampered := noFrag
	q := tampered.Query()
	if tok := q.Get("token"); tok != "" {
		q.Set("token", tok[:len(tok)-2]+"xx")
	}
	tampered.RawQuery = q.Encode()
	for _, tc := range []struct{ name, url, auth string }{
		{"token", noFrag.String(), ""},
		{"token+Authorization", noFrag.String(), "Bearer " + token},
		{"no token", noToken.String(), ""},
		{"no token+Authorization", noToken.String(), "Bearer " + token},
		{"tampered token", tampered.String(), ""},
		{"upload-time url", strings.SplitN(uploadURL, "#", 2)[0], ""},
	} {
		status, h, body := probeGet(t, tc.url, tc.auth)
		t.Logf("FINDING download %s: HTTP %d content-type=%q location=%q matches=%v body=%.120q",
			tc.name, status, h.Get("Content-Type"), h.Get("Location"), bytes.Equal(body, content), body)
	}
	// The same path under /media/ straight on the back, with the documented protected prefix.
	for _, p := range []string{"/media/" + strings.TrimPrefix(u.Path, "/media/"), "/static/" + strings.TrimPrefix(u.Path, "/media/")} {
		status, _, _ := probeGet(t, base.Scheme+"://"+base.Host+p, "")
		t.Logf("FINDING download path %s: HTTP %d", p, status)
	}
}

func keys(v url.Values) []string {
	out := []string{}
	for k := range v {
		out = append(out, k)
	}
	return out
}
