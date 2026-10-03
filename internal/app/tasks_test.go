package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// taskAPI is a stateful Taiga with tasks: by_ref, list (ignoring every filter, so the local
// check is what narrows), GET, PATCH with per-field OCC left to the caller, and POST.
type taskAPI struct {
	noTransfer
	tasks    map[int64]Object
	statuses string
	requests []taiga.Request
	queries  []url.Values
	nextID   int64
	// postErr answers the POST with this error; postApplied stores the task anyway.
	postErr     error
	postApplied bool
	postBody    string // a 2xx POST answer that replaces the created task
	// onPost runs after a POST is stored (to simulate a concurrent create); onPostSent runs when
	// the POST arrives, stored or not (another process creating in the meantime).
	onPost     func()
	onPostSent func()
	// patchErr answers the PATCH (WriteVersionedFrom) with this error; patchApplied applies it anyway.
	patchErr     error
	patchApplied bool
	readErr      error // answers GET tasks/<id> after a write
	written      bool
	snapshots    []map[string]json.RawMessage
}

func newTaskAPI(t *testing.T) (*taskAPI, *Service) {
	t.Helper()
	f := &taskAPI{nextID: 9200, tasks: map[int64]Object{
		9100: {"id": json.Number("9100"), "ref": json.Number("250"), "project": json.Number("37"), "version": json.Number("4"), "subject": "Escrever testes",
			"description": "linha", "user_story": json.Number("6808"), "status": json.Number("11"), "is_closed": false, "tags": []any{[]any{"cli", nil}},
			"assigned_to": json.Number("5"), "owner": json.Number("5"), "is_blocked": false, "blocked_note": "", "due_date": nil},
		9101: {"id": json.Number("9101"), "ref": json.Number("251"), "project": json.Number("37"), "version": json.Number("1"), "subject": "Revisar",
			"description": "", "user_story": json.Number("6809"), "status": json.Number("13"), "is_closed": true, "tags": []any{},
			"assigned_to": nil, "owner": json.Number("6"), "is_blocked": false, "blocked_note": "", "due_date": nil},
		9102: {"id": json.Number("9102"), "ref": json.Number("252"), "project": json.Number("37"), "version": json.Number("2"), "subject": "Alpha tests",
			"description": "", "user_story": json.Number("6808"), "status": json.Number("12"), "is_closed": false, "tags": []any{[]any{"go", nil}, []any{"cli", nil}},
			"assigned_to": json.Number("6"), "owner": json.Number("5"), "is_blocked": false, "blocked_note": "", "due_date": "2026-10-31"},
	}, statuses: `[{"id":11,"name":"New","is_closed":false,"project":37},{"id":12,"name":"In progress","is_closed":false,"project":37},{"id":13,"name":"Closed","is_closed":true,"project":37}]`}
	s, err := New(context.Background(), f, "infra")
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func (f *taskAPI) Do(_ context.Context, r taiga.Request) (*taiga.Response, error) {
	f.requests = append(f.requests, r)
	switch {
	case r.Method == "GET" && r.Path == "projects/by_slug":
		return answer(Object{"id": 37, "slug": "infra"})
	case r.Method == "GET" && r.Path == "users/me":
		return answer(Object{"id": 5, "username": "admin"})
	case r.Method == "GET" && r.Path == "tasks/by_ref":
		for _, t := range f.tasks {
			if fmt.Sprint(t["project"]) == r.Query.Get("project") && fmt.Sprint(t["ref"]) == r.Query.Get("ref") {
				return answer(t)
			}
		}
	case r.Method == "GET" && strings.HasPrefix(r.Path, "tasks/"):
		if f.readErr != nil && f.written {
			return nil, f.readErr
		}
		if t, ok := f.tasks[ID(strings.TrimPrefix(r.Path, "tasks/"))]; ok {
			return answer(t)
		}
	case r.Method == "POST" && r.Path == "tasks":
		f.written = true
		if f.onPostSent != nil {
			f.onPostSent()
		}
		body := r.Body.(Object)
		if f.postErr == nil || f.postApplied {
			f.nextID++
			t := Object{"id": json.Number(fmt.Sprint(f.nextID)), "ref": json.Number("300"), "project": json.Number("37"), "version": json.Number("1"),
				"owner": json.Number("5"), "tags": []any{}, "is_closed": false}
			for k, v := range body {
				if k != "project" {
					t[k] = jsonValue(v)
				}
			}
			f.tasks[f.nextID] = t
			if f.onPost != nil {
				f.onPost()
			}
			if f.postErr == nil && f.postBody != "" {
				return &taiga.Response{Status: 201, Body: []byte(f.postBody)}, nil
			}
			if f.postErr == nil {
				return answer(t)
			}
		}
		return nil, f.postErr
	}
	return nil, &taiga.APIError{Status: 404, Method: r.Method, Path: r.Path}
}

// jsonValue makes a request value look like a decoded answer (numbers as json.Number).
func jsonValue(v any) any {
	b, _ := json.Marshal(v)
	o, err := Decode([]byte(`{"v":` + string(b) + `}`))
	if err != nil {
		return v
	}
	return o["v"]
}

func answer(o Object) (*taiga.Response, error) {
	b, err := json.Marshal(o)
	return &taiga.Response{Status: 200, Body: b}, err
}

func (f *taskAPI) GetAll(_ context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	f.queries = append(f.queries, q)
	if path == "task-statuses" {
		var out []json.RawMessage
		return out, json.Unmarshal([]byte(f.statuses), &out)
	}
	if path != "tasks" {
		return nil, &taiga.APIError{Status: 404, Method: "GET", Path: path}
	}
	ids := []int64{}
	for id := range f.tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []json.RawMessage{}
	for _, id := range ids {
		b, _ := json.Marshal(f.tasks[id])
		out = append(out, b)
	}
	return out, nil
}

func (f *taskAPI) WriteVersioned(context.Context, string, string, map[string]any, bool) (*taiga.Response, error) {
	return nil, errors.New("unexpected write")
}

// WriteVersionedFrom applies patch to the task (the retry itself is the client's, tested there).
func (f *taskAPI) WriteVersionedFrom(_ context.Context, method, path string, patch map[string]any, first map[string]json.RawMessage, _ bool) (*taiga.Response, error) {
	f.requests = append(f.requests, taiga.Request{Method: method, Path: path, Body: patch})
	f.snapshots = append(f.snapshots, first)
	f.written = true
	t := f.tasks[ID(strings.TrimPrefix(path, "tasks/"))]
	if f.patchErr == nil || f.patchApplied {
		for k, v := range patch {
			if k == "tags" {
				pairs := []any{}
				for _, n := range v.([]string) {
					pairs = append(pairs, []any{n, nil})
				}
				v = pairs
			}
			t[k] = jsonValue(v)
		}
		t["version"] = json.Number(fmt.Sprint(ID(t["version"]) + 1))
	}
	if f.patchErr != nil {
		return nil, f.patchErr
	}
	return answer(t)
}

func (f *taskAPI) BaseURL() string { return "http://taiga.test" }

func (f *taskAPI) writes() []taiga.Request {
	out := []taiga.Request{}
	for _, r := range f.requests {
		if r.Method != "GET" {
			out = append(out, r)
		}
	}
	return out
}

var (
	lost     = &taiga.NetworkError{Method: "POST", Path: "tasks", Err: errors.New("connection reset by peer")}
	notSent  = &taiga.NetworkError{Method: "POST", Path: "tasks", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	gateway  = &taiga.APIError{Status: 502, Method: "POST", Path: "tasks"}
	redirect = &taiga.APIError{Status: 302, Method: "POST", Path: "tasks"}
)

func TestTaskByRefAndView(t *testing.T) {
	f, s := newTaskAPI(t)
	o, err := s.Task(context.Background(), "250", 0)
	if err != nil {
		t.Fatal(err)
	}
	last := f.requests[len(f.requests)-1]
	if last.Path != "tasks/by_ref" || last.Query.Get("project") != "37" || last.Query.Get("ref") != "250" {
		t.Fatalf("request: %+v", last)
	}
	v, err := s.TaskView(o)
	if err != nil {
		t.Fatal(err)
	}
	if v["url"] != "http://taiga.test/project/infra/task/250" || fmt.Sprint(v["tags"]) != "[cli]" {
		t.Fatalf("view: %v %v", v["url"], v["tags"])
	}
	if _, err := s.Task(context.Background(), "246", 0); exitOf(err) != output.ExitNotFound {
		t.Fatalf("a story ref is not a task: %v", err)
	}
}

func TestTaskIDFromAnotherProjectIsRejected(t *testing.T) {
	f, s := newTaskAPI(t)
	f.tasks[9300] = Object{"id": json.Number("9300"), "ref": json.Number("250"), "project": json.Number("38"), "version": json.Number("1")}
	if _, err := s.Task(context.Background(), "", 9300); exitOf(err) != output.ExitUsage {
		t.Fatalf("other project: %v", err)
	}
}

func TestTasksFilterLocally(t *testing.T) {
	_, s := newTaskAPI(t)
	for name, c := range map[string]struct {
		filters url.Values
		want    string
	}{
		"story":    {url.Values{"story": {"6808"}}, "[250 252]"},
		"status":   {url.Values{"status": {"12"}}, "[252]"},
		"assignee": {url.Values{"assignee": {"6"}}, "[252]"},
		"closed":   {url.Values{"closed": {"true"}}, "[251]"},
		"open":     {url.Values{"closed": {"false"}}, "[250 252]"},
		"tag":      {url.Values{"tag": {"go", "cli"}}, "[252]"},
		"search":   {url.Values{"search": {"alpha"}}, "[252]"},
		"ref":      {url.Values{"ref": {"251"}}, "[251]"},
	} {
		items, err := s.Tasks(context.Background(), c.filters)
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, o := range items {
			refs = append(refs, fmt.Sprint(o["ref"]))
		}
		if got := "[" + strings.Join(refs, " ") + "]"; got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func TestTasksSendProvenRemoteFilters(t *testing.T) {
	f, s := newTaskAPI(t)
	if _, err := s.Tasks(context.Background(), url.Values{"story": {"6808"}, "status": {"11"}, "assignee": {"5"}, "closed": {"false"}, "tag": {"cli"}, "search": {"x"}}); err != nil {
		t.Fatal(err)
	}
	q := f.queries[len(f.queries)-1]
	want := url.Values{"project": {"37"}, "user_story": {"6808"}, "status": {"11"}, "assigned_to": {"5"}, "status__is_closed": {"false"}}
	if q.Encode() != want.Encode() {
		t.Fatalf("query %s, want %s", q.Encode(), want.Encode())
	}
}

func story6808() Object {
	return Object{"id": json.Number("6808"), "ref": json.Number("246"), "project": json.Number("37")}
}

func TestCreateTaskPostsWithoutVersionAndRereads(t *testing.T) {
	f, s := newTaskAPI(t)
	got, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova", "tags": []string{"cli"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if len(w) != 1 || w[0].Method != "POST" || w[0].Path != "tasks" {
		t.Fatalf("writes: %+v", w)
	}
	body := w[0].Body.(Object)
	if _, ok := body["version"]; ok || fmt.Sprint(body["user_story"]) != "6808" || fmt.Sprint(body["project"]) != "37" {
		t.Fatalf("body: %v", body)
	}
	if v := got.(Object); v["url"] != "http://taiga.test/project/infra/task/300" {
		t.Fatalf("view: %v", v)
	}
}

func TestCreateTaskDryRun(t *testing.T) {
	f, s := newTaskAPI(t)
	got, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := got.(WritePlan); !ok || p.Method != "POST" || p.Path != "tasks" || fmt.Sprint(p.Body["user_story"]) != "6808" || len(f.writes()) != 0 {
		t.Fatalf("plan: %#v writes %v", got, f.writes())
	}
}

func TestCreateTaskUnreadableAnswerIsWriteApplied(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postBody = "<html>proxy</html>"
	_, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false)
	if codeOf(err) != "write_applied" || exitOf(err) != output.ExitUnexpected || !strings.Contains(output.AsError(err).Recovery, "taiga task list --story 246") {
		t.Fatalf("err: %#v", err)
	}
}

func TestCreateTaskUncertainOutcomeFindsTheTask(t *testing.T) {
	for name, sendErr := range map[string]error{"network": lost, "502": gateway, "302": redirect} {
		f, s := newTaskAPI(t)
		f.postErr, f.postApplied = sendErr, true
		got, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Escrever testes"}, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// 9100 has the same subject, story and owner, but existed before the POST.
		if v := got.(Object); fmt.Sprint(v["id"]) != "9201" || len(f.writes()) != 1 {
			t.Fatalf("%s: %v, writes %d", name, v["id"], len(f.writes()))
		}
		if len(s.Warnings) != 1 || s.Warnings[0].Code != "task_create_matched" || !strings.Contains(s.Warnings[0].Message, "#300") {
			t.Fatalf("%s: warnings %+v", name, s.Warnings)
		}
	}
}

func TestCreateTaskUncertainOutcomeNotFoundIsUnconfirmed(t *testing.T) {
	for name, sendErr := range map[string]error{"network": lost, "502": gateway, "302": redirect} {
		f, s := newTaskAPI(t)
		f.postErr = sendErr
		_, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false)
		e := output.AsError(err)
		if e.Code != "task_create_unconfirmed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Recovery, "taiga task list --story 246") || len(f.writes()) != 1 {
			t.Fatalf("%s: %#v writes %d", name, e, len(f.writes()))
		}
	}
}

func TestCreateTaskUncertainOutcomeTwoCandidatesIsUnconfirmed(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postErr, f.postApplied = lost, true
	f.onPost = func() {
		f.tasks[9999] = Object{"id": json.Number("9999"), "ref": json.Number("301"), "project": json.Number("37"), "version": json.Number("1"),
			"subject": "Nova", "user_story": json.Number("6808"), "owner": json.Number("5"), "tags": []any{}}
	}
	if _, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false); codeOf(err) != "task_create_unconfirmed" {
		t.Fatalf("err: %v", err)
	}
}

func TestCreateTaskNotSentIsNetworkError(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postErr = notSent
	if _, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false); exitOf(err) != output.ExitNetwork {
		t.Fatalf("err: %v", err)
	}
}

func TestCreateTaskRefusedIsNotSearched(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postErr = &taiga.APIError{Status: 400, Method: "POST", Path: "tasks", Body: []byte(`{"due_date":["Date has wrong format."]}`)}
	if _, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false); codeOf(err) != "invalid_request" {
		t.Fatalf("err: %v", err)
	}
}

func TestUpdateTaskSendsEveryDependentField(t *testing.T) {
	note := "esperando o B6"
	text := "mais"
	for name, c := range map[string]struct {
		patch Patch
		want  string
	}{
		"assignee":    {Patch{Set: Object{"assigned_to": int64(6)}}, `{"assigned_to":6}`},
		"clear":       {Patch{Set: Object{"assigned_to": nil}}, `{"assigned_to":null}`},
		"block":       {Patch{Set: Object{}, Block: &note}, `{"blocked_note":"esperando o B6","is_blocked":true}`},
		"due":         {Patch{Set: Object{"due_date": "2026-12-31"}}, `{"due_date":"2026-12-31"}`},
		"clear due":   {Patch{Set: Object{"due_date": nil}}, ``},
		"append":      {Patch{Set: Object{}, Append: &text}, `{"description":"linha\n\nmais"}`},
		"add tag":     {Patch{Set: Object{}, AddTags: []string{"go"}}, `{"tags":["cli","go"]}`},
		"same status": {Patch{Set: Object{"status": json.Number("11")}}, ``},
	} {
		f, s := newTaskAPI(t)
		if _, err := s.UpdateTask(context.Background(), "250", c.patch, false, false); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		w := f.writes()
		got := ""
		if len(w) == 1 {
			b, _ := json.Marshal(w[0].Body)
			got = string(b)
		}
		if len(w) > 1 || got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// Taiga records assigned_to in the history diff of a task, so its per-field OCC protects it
// (docs/api-notes.md, "Fase 3 — tasks"): no fence of the #247 kind, the guarded retry instead.
func TestUpdateTaskAssigneeUsesTheGuardedRetry(t *testing.T) {
	f, s := newTaskAPI(t)
	if _, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{"assigned_to": int64(6)}}, false, false); err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, r := range f.requests {
		if r.Method == "GET" && r.Path == "tasks/9100" {
			reads++
		}
	}
	if len(f.snapshots) != 1 || string(f.snapshots[0]["assigned_to"]) != "5" || reads != 1 {
		t.Fatalf("snapshots %v, reads of the task %d (only the one after the write)", f.snapshots, reads)
	}
}

func TestUpdateTaskUncertainPatchConfirmedByReread(t *testing.T) {
	for name, sendErr := range map[string]error{"network": lost, "502": gateway, "302": redirect} {
		f, s := newTaskAPI(t)
		f.patchErr, f.patchApplied = sendErr, true
		text := "mais"
		got, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{}, Append: &text}, false, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v := got.(Object); v["description"] != "linha\n\nmais" {
			t.Fatalf("%s: %v", name, v["description"])
		}
	}
}

func TestUpdateTaskUncertainPatchNotSeenIsUnconfirmed(t *testing.T) {
	for name, sendErr := range map[string]error{"network": lost, "502": gateway, "302": redirect} {
		f, s := newTaskAPI(t)
		f.patchErr = sendErr
		text := "mais"
		_, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{}, Append: &text}, false, false)
		e := output.AsError(err)
		if e.Code != "task_update_unconfirmed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Recovery, "taiga task get 250") {
			t.Fatalf("%s: %#v", name, e)
		}
	}
}

func TestUpdateTaskUncertainPatchUnreadableIsUnconfirmed(t *testing.T) {
	f, s := newTaskAPI(t)
	f.patchErr, f.patchApplied, f.readErr = lost, true, gateway
	_, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{"subject": "x"}}, false, false)
	if codeOf(err) != "task_update_unconfirmed" || exitOf(err) != output.ExitUnexpected {
		t.Fatalf("err: %v", err)
	}
}

func TestUpdateTaskNotSentIsNetworkError(t *testing.T) {
	f, s := newTaskAPI(t)
	f.patchErr = notSent
	if _, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{"subject": "x"}}, false, false); exitOf(err) != output.ExitNetwork {
		t.Fatalf("err: %v", err)
	}
}

func TestUpdateTaskAppliedButUnreadableNamesTheTask(t *testing.T) {
	f, s := newTaskAPI(t)
	f.readErr = gateway
	f.tasks[9100]["subject"] = "Escrever testes"
	api := &truncatedPatch{taskAPI: f}
	s.API = api
	_, err := s.UpdateTask(context.Background(), "250", Patch{Set: Object{"subject": "x"}}, false, false)
	if codeOf(err) != "write_applied" || !strings.Contains(output.AsError(err).Recovery, "taiga task get") {
		t.Fatalf("err: %#v", err)
	}
}

// truncatedPatch answers the PATCH with a 2xx whose body does not decode.
type truncatedPatch struct{ *taskAPI }

func (p *truncatedPatch) WriteVersionedFrom(ctx context.Context, method, path string, patch map[string]any, first map[string]json.RawMessage, force bool) (*taiga.Response, error) {
	if _, err := p.taskAPI.WriteVersionedFrom(ctx, method, path, patch, first, force); err != nil {
		return nil, err
	}
	return &taiga.Response{Status: 200, Body: []byte("<html>")}, nil
}

func TestCloseTaskOnlyChangesTheStatus(t *testing.T) {
	f, s := newTaskAPI(t)
	if _, err := s.CloseTask(context.Background(), "250", "", false, false); err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if b, _ := json.Marshal(w[0].Body); len(w) != 1 || string(b) != `{"status":"13"}` && string(b) != `{"status":13}` {
		t.Fatalf("writes: %+v", w)
	}
	if fmt.Sprint(f.tasks[9100]["tags"]) != "[[cli <nil>]]" {
		t.Fatalf("tags changed: %v", f.tasks[9100]["tags"])
	}
	// Already closed: nothing is sent.
	f2, s2 := newTaskAPI(t)
	if _, err := s2.CloseTask(context.Background(), "251", "", false, false); err != nil || len(f2.writes()) != 0 {
		t.Fatalf("closed task: %v %v", err, f2.writes())
	}
}

func TestCloseTaskWithTwoClosedStatusesNeedsOne(t *testing.T) {
	f, s := newTaskAPI(t)
	f.statuses = `[{"id":11,"name":"New","is_closed":false,"project":37},{"id":13,"name":"Closed","is_closed":true,"project":37},{"id":14,"name":"Rejected","is_closed":true,"project":37}]`
	if _, err := s.CloseTask(context.Background(), "250", "", false, false); codeOf(err) != "ambiguous_name" || len(f.writes()) != 0 {
		t.Fatalf("err: %v", err)
	}
	if _, err := s.CloseTask(context.Background(), "250", "New", false, false); exitOf(err) != output.ExitUsage {
		t.Fatalf("open status: %v", err)
	}
	if _, err := s.CloseTask(context.Background(), "250", "Rejected", false, false); err != nil || len(f.writes()) != 1 {
		t.Fatalf("named closed status: %v", err)
	}
}

// another creates, in the meantime, a task of the same story, owner and subject as the request.
func another(f *taskAPI, fields Object) func() {
	return func() {
		o := Object{"id": json.Number("9999"), "ref": json.Number("301"), "project": json.Number("37"), "version": json.Number("1"),
			"subject": "Nova", "description": "", "user_story": json.Number("6808"), "owner": json.Number("5"), "tags": []any{},
			"status": json.Number("11"), "assigned_to": nil, "due_date": nil}
		for k, v := range fields {
			o[k] = v
		}
		f.tasks[9999] = o
	}
}

// A lost POST and a task of another process with the same subject but other fields: the
// candidate is shown for inspection, never adopted.
func TestCreateTaskUncertainOutcomeRejectsACandidateWithOtherFields(t *testing.T) {
	request := Object{"subject": "Nova", "description": "requested by A", "tags": []string{"process-a"}, "due_date": "2026-12-31",
		"status": json.Number("12"), "assigned_to": json.Number("6")}
	for name, theirs := range map[string]Object{
		"description": {"description": "created by B", "tags": []any{[]any{"process-a", nil}}, "due_date": "2026-12-31", "status": json.Number("12"), "assigned_to": json.Number("6")},
		"tags":        {"description": "requested by A", "tags": []any{[]any{"process-b", nil}}, "due_date": "2026-12-31", "status": json.Number("12"), "assigned_to": json.Number("6")},
		"due_date":    {"description": "requested by A", "tags": []any{[]any{"process-a", nil}}, "due_date": nil, "status": json.Number("12"), "assigned_to": json.Number("6")},
		"status":      {"description": "requested by A", "tags": []any{[]any{"process-a", nil}}, "due_date": "2026-12-31", "status": json.Number("11"), "assigned_to": json.Number("6")},
		"assigned_to": {"description": "requested by A", "tags": []any{[]any{"process-a", nil}}, "due_date": "2026-12-31", "status": json.Number("12"), "assigned_to": nil},
	} {
		for _, sendErr := range []error{gateway, redirect, lost} {
			f, s := newTaskAPI(t)
			f.postErr = sendErr
			f.onPostSent = another(f, theirs)
			body := Object{}
			for k, v := range request {
				body[k] = v
			}
			_, err := s.CreateTask(context.Background(), story6808(), body, false)
			e := output.AsError(err)
			if e.Code != "task_create_unconfirmed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Cause, "#301") || !strings.Contains(e.Cause, name) ||
				!strings.Contains(e.Recovery, "taiga task get 301") || len(s.Warnings) != 0 {
				t.Fatalf("%s %v: %#v warnings %v", name, sendErr, e, s.Warnings)
			}
		}
	}
}

// Every field sent matches (tags as Taiga stores them): success, with a warning that says the
// match is a heuristic.
func TestCreateTaskUncertainOutcomeMatchingCandidateWarns(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postErr = gateway
	f.onPostSent = another(f, Object{"description": "requested by A", "tags": []any{[]any{"process-a", nil}}, "due_date": "2026-12-31"})
	got, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova", "description": "requested by A", "tags": []string{"process-a"}, "due_date": "2026-12-31"}, false)
	if err != nil || fmt.Sprint(got.(Object)["id"]) != "9999" {
		t.Fatalf("%v %v", got, err)
	}
	if len(s.Warnings) != 1 || s.Warnings[0].Code != "task_create_matched" || !strings.Contains(s.Warnings[0].Message, "another process") {
		t.Fatalf("warnings %+v", s.Warnings)
	}
}

// The candidate cannot be read to compare: unconfirmed, never the list form.
func TestCreateTaskUncertainOutcomeUnreadableCandidateIsUnconfirmed(t *testing.T) {
	f, s := newTaskAPI(t)
	f.postErr, f.postApplied, f.readErr = gateway, true, gateway
	_, err := s.CreateTask(context.Background(), story6808(), Object{"subject": "Nova"}, false)
	if e := output.AsError(err); e.Code != "task_create_unconfirmed" || !strings.Contains(e.Cause, "#300") {
		t.Fatalf("%#v", e)
	}
}
