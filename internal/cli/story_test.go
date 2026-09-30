package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestStoryUpdateDryRunPreservesTagsAndVersion(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/37":
			_, _ = fmt.Fprint(w, `{"id":37,"slug":"infra-2025"}`)
		case "/api/v1/userstories/by_ref", "/api/v1/userstories/6808":
			_, _ = fmt.Fprint(w, `{"id":6808,"ref":246,"project":37,"version":7,"subject":"a","tags":[["old",null]]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
	out, stderr, code := runIn(t, map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}, "",
		"story", "update", "246", "--project", "37", "--add-tag", "new", "--dry-run")
	if code != 0 {
		t.Fatalf("%d: %s", code, stderr)
	}
	var plan struct {
		Method, Path string
		Body         map[string]any
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Method != "PATCH" || plan.Path != "userstories/6808" || plan.Body["version"] != float64(7) {
		t.Fatalf("%s", out)
	}
	b, _ := json.Marshal(plan.Body["tags"])
	if string(b) != `["old","new"]` {
		t.Fatalf("tags=%s", b)
	}
	for _, call := range *calls {
		if call.method != "GET" {
			t.Fatalf("dry-run: %+v", call)
		}
	}
}

func TestStoryRejectsInvalidSelectorsAndConflictingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"story", "get", "246", "--id", "6808"},
		{"story", "get"},
		{"story", "get", "0"},
		{"story", "get", "abc"},
		{"story", "get", "--id", "0"},
		{"story", "list", "--ref", "-1"},
		{"story", "list", "--status", ""},
		{"story", "list", "--assignee", " "},
		{"story", "list", "--epic", ""},
		{"story", "list", "--search", ""},
		{"story", "update", "246", "--tag", "a", "--add-tag", "b"},
		{"story", "update", "246", "--add-tag", "a", "--remove-tag", "a"},
		{"story", "update", "246", "--add-tag", "A", "--remove-tag", "a"},
		{"story", "update", "246", "--add-tag", " "},
		{"story", "update", "246", "--subject", ""},
		{"story", "update", "246", "--description-file", "x", "--append-description", "y"},
		{"story", "create", "--subject", " "},
		{"story", "create"},
		{"story", "create", "--subject", "s", "--force-version"},
		{"story", "close", "x"},
	} {
		_, stderr, code := runIn(t, nil, "", args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestStoryEpicWriteIsUnsupportedBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{
		{"story", "create", "--subject", "s", "--epic", "4"},
		{"story", "update", "246", "--epic", "4"},
	} {
		_, stderr, code := runIn(t, map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok"}, "", args...)
		if code != 2 || !strings.Contains(stderr, `"unsupported_operation"`) || !strings.Contains(stderr, "related_userstories") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

// storyFake is a stateful Taiga with two projects, OCC on PATCH and paginated lists.
type storyFake struct {
	t        *testing.T
	mu       sync.Mutex
	slug     string
	stories  map[int64]map[string]any
	statuses []map[string]any
	users    []map[string]any
	members  []int64
	epics    []map[string]any
	pageSize int
	fail     map[string]int
	nextID   int64
	srv      string
	onPatch  func(map[string]any)
	// badWrite makes a successful PATCH/POST answer with a body that is not JSON.
	badWrite bool
	// onRead runs on each GET userstories/<id>, before the answer.
	onRead func(map[string]any)
	// acceptStale accepts a PATCH with an old version, as Taiga does when the fields sent did not
	// change since (per-field OCC, docs/api-notes.md).
	acceptStale bool
}

func newStoryFake(t *testing.T) (*storyFake, *[]recorded) {
	f := &storyFake{
		t:    t,
		slug: "infra-2025",
		stories: map[int64]map[string]any{
			6808: {"id": 6808, "ref": 246, "project": 37, "version": 7, "subject": "Stories na CLI", "description": "",
				"status": 3, "is_closed": false, "tags": []any{[]any{"cli", "#fff"}, []any{"go", nil}}, "assigned_users": []any{5}, "unknown_key": "kept",
				"epics": []any{map[string]any{"id": 90, "ref": 9}}, "milestone": nil, "swimlane": nil, "total_points": json.Number("12345678901234567891")},
			6809: {"id": 6809, "ref": 247, "project": 37, "version": 2, "subject": "Responsáveis", "description": "linha",
				"status": 5, "is_closed": true, "tags": []any{[]any{"cli", nil}}, "assigned_users": []any{}, "epics": nil},
			6810: {"id": 6810, "ref": 248, "project": 37, "version": 1, "subject": "Campos", "description": "",
				"status": 1, "is_closed": false, "tags": []any{}, "assigned_users": []any{6}, "epics": nil},
			7001: {"id": 7001, "ref": 246, "project": 38, "version": 1, "subject": "other project", "tags": []any{}},
		},
		statuses: []map[string]any{
			{"id": 1, "name": "New", "is_closed": false, "project": 37},
			{"id": 3, "name": "In progress", "is_closed": false, "project": 37},
			{"id": 5, "name": "Done", "is_closed": true, "project": 37},
			{"id": 6, "name": "Archived", "is_closed": true, "project": 37},
			{"id": 7, "name": "Dup", "is_closed": false, "project": 37},
			{"id": 8, "name": "Dup", "is_closed": false, "project": 37},
		},
		users:    []map[string]any{{"id": 5, "username": "admin"}, {"id": 6, "username": "svc"}, {"id": 9, "username": "outsider"}},
		members:  []int64{5, 6},
		epics:    []map[string]any{{"id": 90, "ref": 9, "project": 37}, {"id": 9, "ref": 91, "project": 37}},
		pageSize: 2,
		fail:     map[string]int{},
		nextID:   6900,
	}
	srv, calls := fakeTaiga(t, f.handle)
	f.srv = srv.URL
	return f, calls
}

func (f *storyFake) env() map[string]string {
	return map[string]string{"TAIGA_URL": f.srv, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
}

func (f *storyFake) write(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func (f *storyFake) list(w http.ResponseWriter, r *http.Request, items []map[string]any) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page == 0 {
		page = 1
	}
	start := (page - 1) * f.pageSize
	end := min(start+f.pageSize, len(items))
	if end < len(items) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(page+1))
		w.Header().Set("x-pagination-next", f.srv+r.URL.Path+"?"+q.Encode())
	}
	if start > len(items) {
		start = end
	}
	f.write(w, items[start:end])
}

func (f *storyFake) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	q := r.URL.Query()
	if status, ok := f.fail[r.Method+" "+path]; ok {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, `{"_error_message":"injected"}`)
		return
	}
	project := func() bool {
		if q.Get("project") != "37" {
			f.t.Errorf("%s %s without project=37: %s", r.Method, path, r.URL.RawQuery)
			return false
		}
		return true
	}
	switch {
	case r.Method == "GET" && (path == "projects/37" || path == "projects/by_slug" && q.Get("slug") == f.slug):
		f.write(w, map[string]any{"id": 37, "slug": f.slug})
	case r.Method == "GET" && path == "userstory-statuses" && project():
		f.list(w, r, f.statuses)
	case r.Method == "GET" && path == "users" && project():
		f.list(w, r, f.users)
	case r.Method == "GET" && path == "memberships" && project():
		out := []map[string]any{}
		for _, id := range f.members {
			out = append(out, map[string]any{"user": id, "project": 37})
		}
		f.list(w, r, out)
	case r.Method == "GET" && path == "users/me":
		f.write(w, map[string]any{"id": 5, "username": "admin"})
	case r.Method == "GET" && path == "epics" && project():
		f.list(w, r, f.epics)
	case r.Method == "GET" && path == "userstories" && project():
		ids := []int64{}
		for id, s := range f.stories {
			if fmt.Sprint(s["project"]) == "37" {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		items := []map[string]any{}
		for _, id := range ids {
			items = append(items, f.stories[id])
		}
		f.list(w, r, items)
	case r.Method == "GET" && path == "userstories/by_ref":
		for _, s := range f.stories {
			if fmt.Sprint(s["project"]) == q.Get("project") && fmt.Sprint(s["ref"]) == q.Get("ref") {
				f.write(w, s)
				return
			}
		}
		w.WriteHeader(404)
		_, _ = fmt.Fprint(w, `{"_error_message":"No UserStory matches the given query."}`)
	case r.Method == "GET" && strings.HasPrefix(path, "userstories/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, "userstories/"), 10, 64)
		if s, ok := f.stories[id]; ok {
			if f.onRead != nil {
				f.onRead(s)
			}
			f.write(w, s)
			return
		}
		w.WriteHeader(404)
	case r.Method == "PATCH" && strings.HasPrefix(path, "userstories/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, "userstories/"), 10, 64)
		s := f.stories[id]
		if f.onPatch != nil {
			f.onPatch(s)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !f.acceptStale && fmt.Sprint(body["version"]) != fmt.Sprint(s["version"]) {
			w.WriteHeader(400)
			_, _ = fmt.Fprint(w, `{"version":"The version doesn't match with the current one"}`)
			return
		}
		for k, v := range body {
			if k == "tags" {
				pairs := []any{}
				for _, n := range v.([]any) {
					pairs = append(pairs, []any{n, nil})
				}
				v = pairs
			}
			if k != "version" {
				s[k] = v
			}
		}
		s["version"] = s["version"].(int) + 1
		if f.badWrite {
			_, _ = fmt.Fprint(w, `<html>proxy</html>`)
			return
		}
		f.write(w, s)
	case r.Method == "POST" && path == "userstories":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.nextID++
		s := map[string]any{"id": f.nextID, "ref": 300, "project": 37, "version": 1, "tags": []any{}, "status": 1}
		for k, v := range body {
			if k != "tags" && k != "project" {
				s[k] = v
			}
		}
		if tags, ok := body["tags"].([]any); ok {
			for _, n := range tags {
				s["tags"] = append(s["tags"].([]any), []any{n, nil})
			}
		}
		f.stories[f.nextID] = s
		w.WriteHeader(201)
		if f.badWrite {
			_, _ = fmt.Fprint(w, `<html>proxy</html>`)
			return
		}
		f.write(w, s)
	default:
		f.t.Errorf("unexpected %s %s?%s", r.Method, path, r.URL.RawQuery)
		w.WriteHeader(404)
	}
}

func writes(calls *[]recorded) []recorded {
	out := []recorded{}
	for _, c := range *calls {
		if c.method != "GET" {
			out = append(out, c)
		}
	}
	return out
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestStoryGetByRefAndIDGolden(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "get", "246")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "story_get.json", strings.ReplaceAll(out, f.srv, "http://taiga.test"))
	byID, _, code := runIn(t, f.env(), "", "story", "get", "--id", "6808")
	if code != 0 || byID != out {
		t.Fatalf("get --id differs: %d\n%s", code, byID)
	}
	text, _, code := runIn(t, f.env(), "", "story", "get", "246", "--output", "text")
	if code != 0 {
		t.Fatal(code)
	}
	golden(t, "story_get.txt", strings.ReplaceAll(text, f.srv, "http://taiga.test"))
	if len(writes(calls)) != 0 {
		t.Fatalf("get wrote: %+v", writes(calls))
	}
}

func TestStoryGetEscapesSlugInURL(t *testing.T) {
	f, _ := newStoryFake(t)
	f.slug = "a b/ç"
	env := f.env()
	env["TAIGA_PROJECT"] = "37"
	out, stderr, code := runIn(t, env, "", "story", "get", "246")
	if code != 0 || !strings.Contains(out, `"url": "`+f.srv+`/project/a%20b%2F%C3%A7/us/246"`) {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}

func TestStoryIDFromAnotherProjectIsRejectedWithoutWrites(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, args := range [][]string{{"story", "get", "--id", "7001"}, {"story", "get", "999"}} {
		_, stderr, code := runIn(t, f.env(), "", args...)
		if code == 0 || len(writes(calls)) != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "get", "--id", "7001")
	if code != 2 || !strings.Contains(stderr, "another project") {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "get", "999")
	if code != 5 {
		t.Fatalf("missing ref: %d %s", code, stderr)
	}
}

func TestStoryListPaginatesAndFilters(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "list")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "story_list.json", strings.ReplaceAll(out, f.srv, "http://taiga.test"))

	refs := func(args ...string) string {
		t.Helper()
		out, stderr, code := runIn(t, f.env(), "", append([]string{"story", "list"}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(out), &items); err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, it := range items {
			got = append(got, fmt.Sprint(it["ref"]))
		}
		return strings.Join(got, ",")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--ref", "247"}, "247"},
		{[]string{"--ref", "0247"}, "247"},
		{[]string{"--epic", "009"}, "246"},
		{[]string{"--status", "In progress"}, "246"},
		{[]string{"--status", "5"}, "247"},
		{[]string{"--assignee", "me"}, "246"},
		{[]string{"--assignee", "svc"}, "248"},
		{[]string{"--epic", "9"}, "246"},
		{[]string{"--tag", "cli"}, "246,247"},
		{[]string{"--tag", "CLI", "--tag", "go"}, "246"},
		{[]string{"--closed"}, "247"},
		{[]string{"--closed=false"}, "246,248"},
		{[]string{"--search", "RESPONS"}, "247"},
		{[]string{"--tag", "cli", "--closed=false", "--status", "3", "--assignee", "admin", "--epic", "9", "--search", "stories", "--ref", "246"}, "246"},
		{[]string{"--tag", "nope"}, ""},
	} {
		if got := refs(tc.args...); got != tc.want {
			t.Errorf("%v: got %q want %q", tc.args, got, tc.want)
		}
	}
	out, _, _ = runIn(t, f.env(), "", "story", "list", "--tag", "nope")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty list = %q", out)
	}
	// Remote filters proven in the probe are sent; the result is still checked locally.
	last := ""
	for _, c := range *calls {
		if c.path == "/api/v1/userstories" {
			last = c.query
		}
	}
	q, _ := url.ParseQuery(last)
	if q.Get("project") != "37" || len(q["page"]) != 1 {
		t.Fatalf("list query %q", last)
	}
	for _, args := range [][]string{{"--assignee", "outsider"}, {"--assignee", "nobody"}, {"--epic", "90"}, {"--status", "Dup"}, {"--status", "Nope"}} {
		_, stderr, code := runIn(t, f.env(), "", append([]string{"story", "list"}, args...)...)
		if code == 0 {
			t.Errorf("%v accepted: %s", args, stderr)
		}
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "list", "--status", "Dup")
	if code != 2 || !strings.Contains(stderr, "ambiguous_name") {
		t.Fatalf("dup status: %d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "list", "--assignee", "outsider")
	if code != 5 || !strings.Contains(stderr, "not_found") {
		t.Fatalf("non-member: %d %s", code, stderr)
	}
}

func TestStoryListSendsProvenRemoteFilters(t *testing.T) {
	f, calls := newStoryFake(t)
	_, stderr, code := runIn(t, f.env(), "", "story", "list", "--status", "In progress", "--assignee", "me", "--epic", "9", "--closed=false")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	for _, c := range *calls {
		if c.path != "/api/v1/userstories" {
			continue
		}
		q, _ := url.ParseQuery(c.query)
		if q.Get("status") != "3" || q.Get("assigned_users") != "5" || q.Get("epic") != "90" || q.Get("status__is_closed") != "false" {
			t.Fatalf("remote filters %q", c.query)
		}
		return
	}
	t.Fatal("no list call")
}

func TestStoryUpdateWritesDescriptionFromStdinExactly(t *testing.T) {
	f, calls := newStoryFake(t)
	text := "linha \"citada\"\n\tção\u0001fim\n"
	out, stderr, code := runIn(t, f.env(), text, "story", "update", "246", "--description-file", "-", "--status", "Done")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || w[0].path != "/api/v1/userstories/6808" || w[0].body["description"] != text || w[0].body["status"] != float64(5) || w[0].body["version"] != float64(7) {
		t.Fatalf("%+v", w)
	}
	if len(w[0].body) != 3 {
		t.Fatalf("patch is not minimal: %v", w[0].body)
	}
	last := (*calls)[len(*calls)-1]
	if last.method != "GET" || last.path != "/api/v1/userstories/6808" {
		t.Fatalf("no re-read after PATCH: %+v", last)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["description"] != text || got["version"] != float64(8) {
		t.Fatalf("%s", out)
	}
}

func TestStoryUpdateAppendDescription(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, tc := range []struct{ ref, text, want string }{
		{"246", "primeira", "primeira"},
		{"247", "nova", "linha\n\nnova"},
	} {
		_, stderr, code := runIn(t, f.env(), "", "story", "update", tc.ref, "--append-description", tc.text)
		if code != 0 {
			t.Fatalf("%d %s", code, stderr)
		}
		w := writes(calls)
		if w[len(w)-1].body["description"] != tc.want {
			t.Fatalf("%s: %v", tc.ref, w[len(w)-1].body)
		}
	}
	before := len(writes(calls))
	_, _, code := runIn(t, f.env(), "", "story", "update", "248", "--append-description", "")
	if code != 0 || len(writes(calls)) != before {
		t.Fatalf("empty append wrote: %d", code)
	}
}

func TestStoryUpdateNoopSendsNoPatch(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, args := range [][]string{
		{"story", "update", "246", "--subject", "Stories na CLI"},
		{"story", "update", "246", "--tag", "cli", "--tag", "GO"},
		{"story", "update", "246", "--add-tag", "cli", "--remove-tag", "absent"},
		{"story", "update", "246", "--status", "In progress"},
		{"story", "update", "246"},
	} {
		out, stderr, code := runIn(t, f.env(), "", args...)
		if code != 0 || !strings.Contains(out, `"ref": 246`) {
			t.Fatalf("%v: %d %s %s", args, code, out, stderr)
		}
	}
	if len(writes(calls)) != 0 {
		t.Fatalf("no-op wrote: %+v", writes(calls))
	}
}

func TestStoryUpdateTagsMilestoneDryRunGolden(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--remove-tag", "go", "--add-tag", "Nova", "--subject", "Novo título", "--dry-run")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	golden(t, "story_dry_run.json", out)
	if len(writes(calls)) != 0 {
		t.Fatal("dry-run wrote")
	}
}

func TestStoryUpdateRejectsUnknownOrAmbiguousStatus(t *testing.T) {
	f, calls := newStoryFake(t)
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--status", "Nope")
	if code != 5 || !strings.Contains(stderr, "not_found") {
		t.Fatalf("%d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--status", "Dup")
	if code != 2 || !strings.Contains(stderr, "ambiguous_name") {
		t.Fatalf("%d %s", code, stderr)
	}
	if len(writes(calls)) != 0 {
		t.Fatal("wrote")
	}
}

func TestStoryCreatePostsWithoutVersionAndRereads(t *testing.T) {
	f, calls := newStoryFake(t)
	dry, stderr, code := runIn(t, f.env(), "", "story", "create", "--subject", "Nova", "--tag", "A,b", "--status", "In progress", "--dry-run")
	if code != 0 || len(writes(calls)) != 0 || !strings.Contains(dry, `"method": "POST"`) || strings.Contains(dry, "version") {
		t.Fatalf("dry-run: %d %s %s", code, dry, stderr)
	}
	out, stderr, code := runIn(t, f.env(), "", "story", "create", "--subject", "Nova", "--tag", "A,b", "--status", "In progress")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 1 || w[0].method != "POST" || w[0].body["project"] != float64(37) || w[0].body["status"] != float64(3) {
		t.Fatalf("%+v", w)
	}
	if _, ok := w[0].body["version"]; ok {
		t.Fatal("POST sent a version")
	}
	if b, _ := json.Marshal(w[0].body["tags"]); string(b) != `["a,b"]` {
		t.Fatalf("tags %s", b)
	}
	last := (*calls)[len(*calls)-1]
	if last.method != "GET" || last.path != "/api/v1/userstories/6901" || !strings.Contains(out, `"url"`) {
		t.Fatalf("no re-read: %+v %s", last, out)
	}
}

func TestStoryClose(t *testing.T) {
	f, calls := newStoryFake(t)
	_, stderr, code := runIn(t, f.env(), "", "story", "close", "246")
	if code != 2 || !strings.Contains(stderr, "ambiguous_name") {
		t.Fatalf("two closed statuses: %d %s", code, stderr)
	}
	_, stderr, code = runIn(t, f.env(), "", "story", "close", "246", "--status", "New")
	if code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("open status: %d %s", code, stderr)
	}
	_, _, code = runIn(t, f.env(), "", "story", "close", "247")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("already closed wrote: %d", code)
	}
	_, _, code = runIn(t, f.env(), "", "story", "close", "247", "--status", "Done")
	if code != 0 || len(writes(calls)) != 0 {
		t.Fatalf("already in target wrote: %d", code)
	}
	out, stderr, code := runIn(t, f.env(), "", "story", "close", "246", "--status", "Archived")
	w := writes(calls)
	if code != 0 || len(w) != 1 || w[0].body["status"] != float64(6) || len(w[0].body) != 2 || !strings.Contains(out, `"status": 6`) {
		t.Fatalf("%d %s %+v", code, stderr, w)
	}
	f.statuses = f.statuses[:4]
	f.statuses[3]["is_closed"] = false
	_, stderr, code = runIn(t, f.env(), "", "story", "close", "248")
	if code != 0 || writes(calls)[1].body["status"] != float64(5) {
		t.Fatalf("single closed status: %d %s", code, stderr)
	}
}

func TestStoryHTTPErrorsMapToExitCodes(t *testing.T) {
	for _, tc := range []struct {
		fail   string
		status int
		exit   int
	}{
		{"GET userstories/by_ref", 401, 3},
		{"GET userstories/by_ref", 403, 6},
		{"GET projects/by_slug", 404, 5},
		{"PATCH userstories/6808", 409, 4},
		{"PATCH userstories/6808", 503, 7},
	} {
		f, calls := newStoryFake(t)
		f.fail[tc.fail] = tc.status
		_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--subject", "x")
		if code != tc.exit {
			t.Errorf("%s %d: exit %d %s", tc.fail, tc.status, code, stderr)
		}
		if strings.Contains(stderr, "tok") {
			t.Errorf("token leaked: %s", stderr)
		}
		if n := len(writes(calls)); tc.status == 503 && n != 1 {
			t.Errorf("PATCH repeated after 503: %d", n)
		}
	}
}

func TestStoryConcurrentChangeOfMergedFieldConflicts(t *testing.T) {
	f, calls := newStoryFake(t)
	// Someone else edits the tags between our read and our PATCH.
	f.onPatch = func(s map[string]any) {
		s["tags"] = []any{[]any{"theirs", nil}}
		s["version"] = s["version"].(int) + 1
		f.onPatch = nil
	}
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--add-tag", "mine")
	if code != 4 || !strings.Contains(stderr, "version_conflict") || !strings.Contains(stderr, "tags") {
		t.Fatalf("%d %s", code, stderr)
	}
	if n := len(writes(calls)); n != 1 {
		t.Fatalf("PATCH sent %d times", n)
	}
	if b, _ := json.Marshal(f.stories[6808]["tags"]); string(b) != `[["theirs",null]]` {
		t.Fatalf("concurrent tags overwritten: %s", b)
	}
}

// Once the write succeeded, a failed re-read must not look like a failed write (re-running
// would duplicate the story or the appended text): the write response is printed instead.
func TestStoryWriteSucceedsWhenRereadFails(t *testing.T) {
	f, calls := newStoryFake(t)
	f.fail["GET userstories/6901"] = 503
	out, stderr, code := runIn(t, f.env(), "", "story", "create", "--subject", "Nova")
	if code != 0 || !strings.Contains(out, `"id": 6901`) || !strings.Contains(out, `"url"`) || len(writes(calls)) != 1 {
		t.Fatalf("create: %d %s %s", code, out, stderr)
	}
	f.fail["GET userstories/6808"] = 503
	out, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--append-description", "fim")
	if code != 0 || !strings.Contains(out, `"description": "fim"`) || len(writes(calls)) != 2 {
		t.Fatalf("update: %d %s %s", code, out, stderr)
	}
}

// A write confirmed by its HTTP status whose result cannot be shown must say it was applied,
// keep the re-read failure, and not look like a retryable server error.
func TestStoryWriteAppliedButUnreadable(t *testing.T) {
	f, calls := newStoryFake(t)
	f.badWrite = true
	f.fail["GET userstories/6808"] = 503
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--append-description", "fim")
	if code != 1 || out != "" || !strings.Contains(stderr, `"code": "write_applied"`) || !strings.Contains(stderr, "injected") ||
		!strings.Contains(stderr, "PATCH userstories/6808") || !strings.Contains(stderr, "do not re-run") {
		t.Fatalf("update: %d %q %s", code, out, stderr)
	}
	if f.stories[6808]["description"] != "fim" || len(writes(calls)) != 1 {
		t.Fatalf("write not applied once: %v", f.stories[6808]["description"])
	}
	// Re-read fine: the undecodable write response does not matter.
	delete(f.fail, "GET userstories/6808")
	out, stderr, code = runIn(t, f.env(), "", "story", "update", "246", "--subject", "novo")
	if code != 0 || !strings.Contains(out, `"subject": "novo"`) {
		t.Fatalf("re-read ok: %d %s %s", code, out, stderr)
	}
	// POST confirmed by 201 without a decodable body: the id is unknown, still applied.
	_, stderr, code = runIn(t, f.env(), "", "story", "create", "--subject", "Nova")
	if code != 1 || !strings.Contains(stderr, `"code": "write_applied"`) || !strings.Contains(stderr, "POST userstories") || !strings.Contains(stderr, "do not re-run") {
		t.Fatalf("create: %d %s", code, stderr)
	}
	if n := len(writes(calls)); n != 3 {
		t.Fatalf("writes %d", n)
	}
}
