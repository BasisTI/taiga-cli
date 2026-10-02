package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// fakeAPI serves fixed JSON bodies by path and records every request.
type fakeAPI struct {
	noTransfer
	objects map[string]string
	lists   map[string]string
	queries []url.Values
}

func (f *fakeAPI) Do(_ context.Context, r taiga.Request) (*taiga.Response, error) {
	if body, ok := f.objects[r.Path]; ok {
		return &taiga.Response{Status: 200, Body: []byte(body)}, nil
	}
	return nil, &taiga.APIError{Status: 404, Method: r.Method, Path: r.Path}
}

func (f *fakeAPI) GetAll(_ context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	f.queries = append(f.queries, q)
	var out []json.RawMessage
	err := json.Unmarshal([]byte(f.lists[path]), &out)
	return out, err
}

func (f *fakeAPI) WriteVersioned(context.Context, string, string, map[string]any, bool) (*taiga.Response, error) {
	return nil, errors.New("unexpected write")
}

func (f *fakeAPI) WriteVersionedFrom(context.Context, string, string, map[string]any, map[string]json.RawMessage, bool) (*taiga.Response, error) {
	return nil, errors.New("unexpected write")
}

// noTransfer fails every upload and download; fakes of services that never transfer embed it.
type noTransfer struct{}

func (noTransfer) Upload(context.Context, string, map[string]string, string, string, io.Reader, int64) (*taiga.Response, error) {
	return nil, errors.New("unexpected upload")
}

func (noTransfer) Download(context.Context, string, io.Writer) (int64, error) {
	return 0, errors.New("unexpected download")
}

func (f *fakeAPI) BaseURL() string { return "http://taiga.test" }

func service(t *testing.T, f *fakeAPI) *Service {
	t.Helper()
	if f.objects == nil {
		f.objects = map[string]string{}
	}
	f.objects["projects/by_slug"] = `{"id":37,"slug":"infra"}`
	s, err := New(context.Background(), f, "infra")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func exitOf(err error) int {
	var e *output.Error
	if errors.As(err, &e) {
		return e.Exit
	}
	return -1
}

func TestNamesNormalizesPairsAndRejectsGarbage(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`[["a",null],["b","#fff"],"c"]`), &v)
	got, err := Names(v)
	if err != nil || len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{`[[]]`, `[1]`, `{"a":1}`} {
		_ = json.Unmarshal([]byte(bad), &v)
		if _, err := Names(v); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if got, err := Names(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil tags: %#v", got)
	}
}

func TestMergeNamesKeepsOrderAndDropsDuplicates(t *testing.T) {
	got := MergeNames([]string{"a", "b", "a"}, []string{"c", "b"}, []string{"a"})
	if b, _ := json.Marshal(got); string(b) != `["b","c"]` {
		t.Fatalf("%s", b)
	}
	if got := MergeNames(nil, nil, nil); got == nil {
		t.Fatal("empty merge must be [] not null")
	}
}

func TestBuildPatchIsMinimal(t *testing.T) {
	before, _ := Decode([]byte(`{"version":3,"subject":"s","description":"","status":2,"tags":[["a",null]]}`))
	status, _ := Decode([]byte(`{"id":2}`))
	text := "x"
	empty := ""
	for _, tc := range []struct {
		p    Patch
		want string
	}{
		{Patch{Set: Object{"subject": "s", "status": status["id"], "tags": []string{"a"}}}, `{}`},
		{Patch{AddTags: []string{"a"}, RemoveTags: []string{"z"}}, `{}`},
		{Patch{Append: &empty}, `{}`},
		{Patch{Append: &text}, `{"description":"x"}`},
		{Patch{Set: Object{"subject": "t"}, AddTags: []string{"b"}}, `{"subject":"t","tags":["a","b"]}`},
		{Patch{RemoveTags: []string{"a"}}, `{"tags":[]}`},
	} {
		got, err := BuildPatch(before, tc.p)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := json.Marshal(got); string(b) != tc.want {
			t.Errorf("%+v: got %s want %s", tc.p, b, tc.want)
		}
	}
	withText, _ := Decode([]byte(`{"version":3,"description":"linha"}`))
	got, _ := BuildPatch(withText, Patch{Append: &text})
	if got["description"] != "linha\n\nx" {
		t.Fatalf("%q", got["description"])
	}
}

func TestResolveNotFoundAndAmbiguous(t *testing.T) {
	items := []Object{{"id": json.Number("1"), "name": "New"}, {"id": json.Number("2"), "name": "Dup"}, {"id": json.Number("3"), "name": "Dup"}}
	if o, err := Resolve(items, "2", "name"); err != nil || ID(o["id"]) != 2 {
		t.Fatalf("by id: %v %v", o, err)
	}
	if _, err := Resolve(items, "new", "name"); exitOf(err) != output.ExitNotFound {
		t.Fatalf("name match must be exact: %v", err)
	}
	if _, err := Resolve(items, "Dup", "name"); exitOf(err) != output.ExitUsage {
		t.Fatalf("dup: %v", err)
	}
}

func TestCatalogDropsEntriesOfOtherProjects(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{"userstory-statuses": `[{"id":1,"name":"New","project":37},{"id":9,"name":"New","project":38}]`}}
	s := service(t, f)
	items, err := s.Catalog(context.Background(), "userstory-statuses")
	if err != nil || len(items) != 1 || ID(items[0]["id"]) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	if _, err := s.Catalog(context.Background(), "userstory-statuses"); err != nil || len(f.queries) != 1 {
		t.Fatalf("catalog not cached: %d queries", len(f.queries))
	}
	if f.queries[0].Get("project") != "37" {
		t.Fatalf("query %v", f.queries[0])
	}
}

func TestMemberRequiresMembership(t *testing.T) {
	f := &fakeAPI{
		objects: map[string]string{"users/me": `{"id":6}`},
		lists: map[string]string{
			"users":       `[{"id":5,"username":"admin"},{"id":6,"username":"svc"},{"id":9,"username":"outsider"}]`,
			"memberships": `[{"user":5,"project":37},{"user":6,"project":37}]`,
		},
	}
	s := service(t, f)
	for sel, want := range map[string]int64{"admin": 5, "6": 6, "me": 6} {
		if u, err := s.Member(context.Background(), sel); err != nil || ID(u["id"]) != want {
			t.Errorf("%s: %v %v", sel, u, err)
		}
	}
	for _, sel := range []string{"outsider", "adm", "Admin"} {
		if _, err := s.Member(context.Background(), sel); exitOf(err) != output.ExitNotFound {
			t.Errorf("%s: %v", sel, err)
		}
	}
}

func TestEpicResolvesByRefNeverByID(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{"epics": `[{"id":4,"ref":9,"project":37},{"id":9,"ref":12,"project":37}]`}}
	s := service(t, f)
	if e, err := s.Epic(context.Background(), "9"); err != nil || ID(e["id"]) != 4 {
		t.Fatalf("%v %v", e, err)
	}
	if e, err := s.Epic(context.Background(), "009"); err != nil || ID(e["id"]) != 4 {
		t.Fatalf("leading zeros: %v %v", e, err)
	}
	if _, err := s.Epic(context.Background(), "4"); exitOf(err) != output.ExitNotFound {
		t.Fatalf("id accepted as ref: %v", err)
	}
}

func TestStoryRejectsOtherProjectAndMismatchedIdentity(t *testing.T) {
	f := &fakeAPI{objects: map[string]string{
		"userstories/by_ref": `{"id":1,"ref":5,"project":38,"version":1}`,
		"userstories/2":      `{"id":3,"ref":5,"project":37,"version":1}`,
	}}
	s := service(t, f)
	if _, err := s.Story(context.Background(), "5", 0); exitOf(err) != output.ExitUsage {
		t.Fatalf("other project: %v", err)
	}
	if _, err := s.Story(context.Background(), "", 2); err == nil {
		t.Fatal("GET returned another id and was accepted")
	}
	for _, bad := range []struct {
		ref string
		id  int64
	}{{"", 0}, {"5", 2}, {"0", 0}, {"x", 0}, {"", -1}} {
		if _, err := s.Story(context.Background(), bad.ref, bad.id); exitOf(err) != output.ExitUsage {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestNewRequiresProjectAndUsesIDPath(t *testing.T) {
	if _, err := New(context.Background(), &fakeAPI{}, ""); exitOf(err) != output.ExitUsage {
		t.Fatalf("%v", err)
	}
	f := &fakeAPI{objects: map[string]string{"projects/37": `{"id":37,"slug":"infra"}`}}
	if s, err := New(context.Background(), f, "37"); err != nil || s.Project["slug"] != "infra" {
		t.Fatalf("%v %v", s, err)
	}
}
