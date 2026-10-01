package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// applyFake keeps the project in memory and counts writes.
type applyFake struct {
	writes     int
	denied     bool
	failAt     int // write number that fails, 0 = none
	reorderErr error
	statuses   []Object
	fields     []Object
}

func (f *applyFake) CheckPermission(context.Context) error {
	if f.denied {
		return &taigaForbidden{}
	}
	return nil
}

type taigaForbidden struct{}

func (*taigaForbidden) Error() string { return "admin_project_values required" }

func (f *applyFake) ProjectID() any { return 37 }
func (f *applyFake) CheckReorderSupport(context.Context) error {
	return f.reorderErr
}
func (f *applyFake) Load(context.Context) ([]Object, []Object, error) {
	return append([]Object{}, f.statuses...), append([]Object{}, f.fields...), nil
}
func (f *applyFake) write() error {
	f.writes++
	if f.writes == f.failAt {
		return errors.New("server_error")
	}
	return nil
}
func (f *applyFake) CreateStatus(_ context.Context, body Object) error {
	if err := f.write(); err != nil {
		return err
	}
	st := Object{"id": 100 + f.writes, "order": nextStatusOrder(f.statuses)}
	for k, v := range body {
		st[k] = v
	}
	f.statuses = append(f.statuses, st)
	return nil
}
func (f *applyFake) CreateField(_ context.Context, body Object) error {
	if err := f.write(); err != nil {
		return err
	}
	f.fields = append(f.fields, body)
	return nil
}
func (f *applyFake) Reorder(context.Context, []string) error { return f.write() }

func TestApplyPreflightAndDryRunNeverWrite(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "In revision", Color: "#8E44AD"}}}
	for _, dry := range []bool{true, false} {
		f := &applyFake{statuses: []Object{{"name": "In revision", "color": "#000000", "is_closed": false}}}
		got, err := Apply(context.Background(), f, spec, dry)
		if err == nil || f.writes != 0 || exitOf(err) != 2 || len(got.Plan.Drift) != 1 {
			t.Fatalf("drift: writes=%d err=%v", f.writes, err)
		}
	}
	f := &applyFake{}
	got, err := Apply(context.Background(), f, spec, true)
	if err != nil || f.writes != 0 || len(got.Plan.Actions) != 1 || got.Complete || len(got.Requests) != 1 {
		t.Fatalf("dry: %+v writes=%d err=%v", got, f.writes, err)
	}
	if r := got.Requests[0]; r.Method != "POST" || r.Path != "userstory-statuses" || r.Body["project"] != 37 || r.Body["order"] != int64(1) {
		t.Fatalf("request: %+v", r)
	}
	for _, dry := range []bool{true, false} {
		f = &applyFake{denied: true}
		if _, err := Apply(context.Background(), f, spec, dry); err == nil || f.writes != 0 {
			t.Fatalf("permission: %d %v", f.writes, err)
		}
	}
}

func TestApplyRefusesReorderBeforeAnyWrite(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "In revision", Color: "#8E44AD", After: "In progress"}}}
	for _, dry := range []bool{true, false} {
		f := &applyFake{statuses: remoteStatuses(), reorderErr: Unsupported("no OCC", "")}
		got, err := Apply(context.Background(), f, spec, dry)
		if err == nil || exitOf(err) != 2 || f.writes != 0 || len(got.Remaining) != 2 || len(got.Applied) != 0 {
			t.Fatalf("dry=%v: %+v writes=%d %v", dry, got, f.writes, err)
		}
	}
}

func TestApplyCreatesInFileOrderAndIsIdempotent(t *testing.T) {
	spec := ProjectSpec{
		StoryStatus: []StatusSpec{{Name: "In revision", Color: "#8E44AD"}, {Name: "Waiting for deployment", Color: "#3498DB"}},
		StoryField:  []FieldSpec{{Name: "Testado em staging", Type: "checkbox"}},
	}
	f := &applyFake{statuses: remoteStatuses(), fields: remoteFields()}
	got, err := Apply(context.Background(), f, spec, false)
	if err != nil || !got.Complete || f.writes != 3 || len(got.Applied) != 3 || len(got.Remaining) != 0 {
		t.Fatalf("first: %+v writes=%d %v", got, f.writes, err)
	}
	if f.statuses[4]["order"] != int64(6) || f.statuses[5]["order"] != int64(7) {
		t.Fatalf("orders: %v", f.statuses)
	}
	got, err = Apply(context.Background(), f, spec, false)
	if err != nil || !got.Complete || f.writes != 3 || len(got.Plan.Actions) != 0 || len(got.Plan.Unmanaged) != 5 {
		t.Fatalf("second: %+v writes=%d %v", got, f.writes, err)
	}
}

func TestApplyPartialFailureReportsWhatRemains(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "A", Color: "#000000"}, {Name: "B", Color: "#000000"}, {Name: "C", Color: "#000000"}}}
	f := &applyFake{statuses: remoteStatuses(), failAt: 2}
	got, err := Apply(context.Background(), f, spec, false)
	if err == nil || got.Complete || kinds(got.Applied) != "create_status:A" || kinds(got.Remaining) != "create_status:B,create_status:C" {
		t.Fatalf("partial: %+v %v", got, err)
	}
	// The next run re-plans from Taiga: A is not created twice.
	f.failAt = 0
	got, err = Apply(context.Background(), f, spec, false)
	if err != nil || !got.Complete || kinds(got.Applied) != "create_status:B,create_status:C" || len(f.statuses) != 7 {
		t.Fatalf("resume: %+v %v", got, err)
	}
}

// projectChangingFake creates a different status than asked for: the final re-read differs.
type projectChangingFake struct{ applyFake }

func (f *projectChangingFake) CreateStatus(_ context.Context, body Object) error {
	f.writes++
	f.statuses = append(f.statuses, Object{"id": 99, "name": body["name"], "order": 99, "color": "#FFFFFF", "is_closed": false})
	return nil
}

func TestApplyIsCompleteOnlyAfterTheReRead(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "A", Color: "#000000"}}}
	f := &projectChangingFake{applyFake{statuses: remoteStatuses()}}
	got, err := Apply(context.Background(), f, spec, false)
	if err == nil || exitOf(err) != 4 || got.Complete {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPreviewProject(t *testing.T) {
	plan := ProjectPlan{Actions: []Action{
		{"create_status", "A", Object{"name": "A", "color": "#000000", "is_closed": false}},
		{"create_field", "F", Object{"name": "F", "type": "text", "description": ""}},
		{"create_status", "B", Object{"name": "B", "color": "#000000", "is_closed": false}},
	}}
	requests, deferred, err := PreviewProject(plan, remoteStatuses(), 37)
	if err != nil || len(deferred) != 0 || len(requests) != 3 {
		t.Fatalf("%v %v %v", requests, deferred, err)
	}
	if requests[0].Body["order"] != int64(6) || requests[2].Body["order"] != int64(7) || requests[1].Path != "userstory-custom-attributes" || requests[1].Body["order"] != nil {
		t.Fatalf("%+v", requests)
	}
	reorder := ProjectPlan{Actions: []Action{{"reorder_statuses", "", Object{"names": []string{"Done", "New", "In progress", "Ready for test"}}}}}
	if _, _, err := PreviewProject(reorder, remoteStatuses(), 37); err == nil || exitOf(err) != 2 {
		t.Fatalf("a status without version cannot be reordered: %v", err)
	}
}

// statusAPI serves the status and field catalogs the way the local Taiga 6.7 does.
type statusAPI struct {
	permissions string
	statuses    []Object
	fields      []Object
	posts       []taiga.Request
	fieldPosts  []taiga.Request
	postErr     error
	created     bool // postErr comes after the status was created
}

func (a *statusAPI) Do(_ context.Context, r taiga.Request) (*taiga.Response, error) {
	switch {
	case r.Method == "GET" && r.Path == "projects/37":
		return &taiga.Response{Status: 200, Body: []byte(`{"id":37,"slug":"infra","my_permissions":` + a.permissions + `}`)}, nil
	case r.Method == "GET" && strings.HasPrefix(r.Path, "userstory-statuses/"):
		for _, st := range a.statuses {
			if fmt.Sprintf("userstory-statuses/%v", st["id"]) == r.Path {
				b, _ := json.Marshal(st)
				return &taiga.Response{Status: 200, Body: b}, nil
			}
		}
	case r.Method == "POST" && r.Path == "userstory-custom-attributes":
		a.fieldPosts = append(a.fieldPosts, r)
		f := Object{"id": 80 + len(a.fieldPosts), "project": 37}
		for k, v := range r.Body.(Object) {
			f[k] = v
		}
		a.fields = append(a.fields, f)
		b, _ := json.Marshal(f)
		return &taiga.Response{Status: 201, Body: b}, nil
	case r.Method == "GET" && strings.HasPrefix(r.Path, "userstory-custom-attributes/"):
		for _, f := range a.fields {
			if fmt.Sprintf("userstory-custom-attributes/%v", f["id"]) == r.Path {
				b, _ := json.Marshal(f)
				return &taiga.Response{Status: 200, Body: b}, nil
			}
		}
	case r.Method == "POST" && r.Path == "userstory-statuses":
		a.posts = append(a.posts, r)
		body := r.Body.(Object)
		st := Object{"id": 50 + len(a.posts), "project": 37}
		for k, v := range body {
			st[k] = v
		}
		if a.postErr != nil {
			if a.created {
				a.statuses = append(a.statuses, st)
			}
			return nil, a.postErr
		}
		a.statuses = append(a.statuses, st)
		b, _ := json.Marshal(st)
		return &taiga.Response{Status: 201, Body: b}, nil
	}
	return nil, &taiga.APIError{Status: 404, Method: r.Method, Path: r.Path}
}

func (a *statusAPI) GetAll(_ context.Context, path string, _ url.Values) ([]json.RawMessage, error) {
	items := a.fields
	if path == "userstory-statuses" {
		items = a.statuses
	}
	out := []json.RawMessage{}
	for _, it := range items {
		b, _ := json.Marshal(it)
		out = append(out, b)
	}
	return out, nil
}

func (a *statusAPI) WriteVersioned(context.Context, string, string, map[string]any, bool) (*taiga.Response, error) {
	return nil, errors.New("unexpected write")
}

func (a *statusAPI) WriteVersionedFrom(context.Context, string, string, map[string]any, map[string]json.RawMessage, bool) (*taiga.Response, error) {
	return nil, errors.New("unexpected write")
}

func (a *statusAPI) BaseURL() string { return "http://taiga.test" }

func statusWriter(a *statusAPI) *statusHTTPWriter {
	if a.permissions == "" {
		a.permissions = `["view_project","admin_project_values"]`
	}
	if a.statuses == nil {
		a.statuses = remoteStatuses()
	}
	return &statusHTTPWriter{service: &Service{API: a, Project: Object{"id": 37, "slug": "infra"}, catalogs: map[string][]Object{}}}
}

func TestStatusWriterPermission(t *testing.T) {
	for perms, want := range map[string]int{`["view_project","admin_project_values"]`: 0, `["view_project","modify_us"]`: 6, `null`: 6, `"admin_project_values"`: 6} {
		err := statusWriter(&statusAPI{permissions: perms}).CheckPermission(context.Background())
		if (want == 0) != (err == nil) || (err != nil && exitOf(err) != want) {
			t.Errorf("%s: %v", perms, err)
		}
	}
}

func TestStatusWriterCreateStatus(t *testing.T) {
	desired := Object{"name": "In revision", "color": "#8E44AD", "is_closed": false}
	a := &statusAPI{}
	w := statusWriter(a)
	if err := w.CreateStatus(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	body := a.posts[0].Body.(Object)
	if len(a.posts) != 1 || body["order"] != int64(6) || body["project"] != 37 || body["name"] != "In revision" {
		t.Fatalf("post: %+v", a.posts)
	}
	// Already there and equal: no POST.
	if err := w.CreateStatus(context.Background(), desired); err != nil || len(a.posts) != 1 {
		t.Fatalf("again: %v %d", err, len(a.posts))
	}
	// Already there with another color, or with another case: refused, no POST.
	for _, other := range []Object{{"name": "In revision", "color": "#000000", "is_closed": false}, {"name": "in revision", "color": "#8E44AD", "is_closed": false}} {
		if err := w.CreateStatus(context.Background(), other); err == nil || exitOf(err) != 4 || len(a.posts) != 1 {
			t.Fatalf("%v: %v %d", other, err, len(a.posts))
		}
	}
}

func TestStatusWriterCreateStatusNeverRepeatsThePost(t *testing.T) {
	desired := Object{"name": "X", "color": "#000000", "is_closed": false}
	// Someone else created it first: 400 on name, then the re-read finds an equal status.
	a := &statusAPI{postErr: &taiga.APIError{Status: 400, Method: "POST", Path: "userstory-statuses", Body: []byte(`{"name":["Duplicated name"]}`)}, created: true}
	if err := statusWriter(a).CreateStatus(context.Background(), desired); err != nil || len(a.posts) != 1 {
		t.Fatalf("duplicate: %v %d", err, len(a.posts))
	}
	// The answer was lost after the 201: the re-read finds it.
	a = &statusAPI{postErr: &taiga.UnreadableBodyError{Method: "POST", Path: "userstory-statuses", Status: 201, Err: errors.New("unexpected EOF")}, created: true}
	if err := statusWriter(a).CreateStatus(context.Background(), desired); err != nil || len(a.posts) != 1 {
		t.Fatalf("unreadable, created: %v %d", err, len(a.posts))
	}
	// Lost answer and nothing found: write_applied, not a network error to retry.
	a = &statusAPI{postErr: &taiga.UnreadableBodyError{Method: "POST", Path: "userstory-statuses", Status: 201, Err: errors.New("unexpected EOF")}}
	if err := statusWriter(a).CreateStatus(context.Background(), desired); !strings.Contains(fmt.Sprint(err), "write_applied") || len(a.posts) != 1 {
		t.Fatalf("unreadable, missing: %v %d", err, len(a.posts))
	}
	// A 5xx is not retried.
	a = &statusAPI{postErr: &taiga.APIError{Status: 502, Method: "POST", Path: "userstory-statuses"}}
	if err := statusWriter(a).CreateStatus(context.Background(), desired); exitOf(err) != 7 || len(a.posts) != 1 {
		t.Fatalf("5xx: %v %d", err, len(a.posts))
	}
}

func TestStatusWriterRefusesReorder(t *testing.T) {
	w := statusWriter(&statusAPI{})
	if _, _, err := w.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{w.CheckReorderSupport(context.Background()), w.Reorder(context.Background(), []string{"Done", "New", "In progress", "Ready for test"})} {
		if exitOf(err) != 2 || !strings.Contains(fmt.Sprint(err), "unsupported_operation") {
			t.Fatalf("%v", err)
		}
	}
}

func TestApplyProjectAgainstTheHTTPWriter(t *testing.T) {
	a := &statusAPI{fields: remoteFields()}
	s := &Service{API: a, Project: Object{"id": 37, "slug": "infra"}, catalogs: map[string][]Object{}}
	statusWriter(a)
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "In revision", Color: "#8E44AD", After: "Done"}}}
	got, err := s.ApplyProject(context.Background(), spec, false)
	if err != nil || !got.Complete || len(a.posts) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	spec.StoryStatus[0].After = "In progress"
	got, err = s.ApplyProject(context.Background(), spec, false)
	if exitOf(err) != 2 || len(a.posts) != 1 || got.Complete {
		t.Fatalf("reorder: %+v %v", got, err)
	}
}

func TestStatusWriterCreateFieldRereadsTheCatalog(t *testing.T) {
	desired := Object{"name": "ReviewRace", "type": "text", "description": ""}
	a := &statusAPI{}
	w := statusWriter(a)
	if _, _, err := w.Load(context.Background()); err != nil { // caches an empty field catalog
		t.Fatal(err)
	}
	// Another client creates a case variant after the preflight.
	a.fields = append(a.fields, Object{"id": 70, "name": "reviewrace", "type": "text", "description": "", "project": 37})
	if err := w.CreateField(context.Background(), desired); exitOf(err) != 4 || !strings.Contains(fmt.Sprint(err), "project_changed") || len(a.fieldPosts) != 0 {
		t.Fatalf("case variant: %v %d", err, len(a.fieldPosts))
	}
	// The same name with another type appeared: changed, no POST.
	a.fields = []Object{{"id": 71, "name": "ReviewRace", "type": "date", "description": "", "project": 37}}
	if err := w.CreateField(context.Background(), desired); exitOf(err) != 4 || len(a.fieldPosts) != 0 {
		t.Fatalf("other type: %v %d", err, len(a.fieldPosts))
	}
	// An equal one appeared: no-op. Missing: created once.
	a.fields = []Object{{"id": 72, "name": "ReviewRace", "type": "text", "description": "", "project": 37}}
	if err := w.CreateField(context.Background(), desired); err != nil || len(a.fieldPosts) != 0 {
		t.Fatalf("equal: %v %d", err, len(a.fieldPosts))
	}
	a.fields = nil
	if err := w.CreateField(context.Background(), desired); err != nil || len(a.fieldPosts) != 1 {
		t.Fatalf("create: %v %d", err, len(a.fieldPosts))
	}
}

func TestStatusWriterCreateStatusRefusesAVariantNextToTheExactName(t *testing.T) {
	a := &statusAPI{statuses: append(remoteStatuses(),
		Object{"id": 60, "name": "X", "order": 6, "color": "#000000", "is_closed": false},
		Object{"id": 61, "name": "x", "order": 7, "color": "#000000", "is_closed": false})}
	err := statusWriter(a).CreateStatus(context.Background(), Object{"name": "X", "color": "#000000", "is_closed": false})
	if exitOf(err) != 4 || len(a.posts) != 0 {
		t.Fatalf("%v %d", err, len(a.posts))
	}
}

func TestBuildProjectPlanCaseVariantNextToTheExactName(t *testing.T) {
	fields := append(remoteFields(), Object{"id": 12, "name": "executor", "type": "text", "description": "who"})
	plan, err := BuildProjectPlan(ProjectSpec{StoryField: []FieldSpec{{Name: "Executor", Type: "text", Description: "who"}}}, remoteStatuses(), fields)
	if err != nil || len(plan.Drift) != 1 || len(plan.Actions) != 0 {
		t.Fatalf("field: %+v %v", plan, err)
	}
	statuses := append(remoteStatuses(), Object{"id": 12, "name": "NEW", "order": 9, "color": "#70728F", "is_closed": false})
	plan, err = BuildProjectPlan(ProjectSpec{StoryStatus: []StatusSpec{{Name: "New", Color: "#70728F"}}}, statuses, nil)
	if err != nil || len(plan.Drift) != 1 {
		t.Fatalf("status: %+v %v", plan, err)
	}
}

// fieldRaceFake: while the status is created, another client creates a case variant of the field.
type fieldRaceFake struct{ applyFake }

func (f *fieldRaceFake) CreateStatus(ctx context.Context, body Object) error {
	f.fields = append(f.fields, Object{"id": 70, "name": "reviewrace", "type": "text", "description": ""})
	return f.applyFake.CreateStatus(ctx, body)
}

func TestApplyIsNotCompleteWithACaseCollision(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "ReviewTrigger", Color: "#000000"}}, StoryField: []FieldSpec{{Name: "ReviewRace", Type: "text"}}}
	f := &fieldRaceFake{applyFake{statuses: remoteStatuses()}}
	got, err := Apply(context.Background(), f, spec, false)
	if exitOf(err) != 4 || got.Complete {
		t.Fatalf("%+v %v", got, err)
	}
}
