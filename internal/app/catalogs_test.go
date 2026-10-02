package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func catalogNames(t *testing.T, items []Object, key string) string {
	t.Helper()
	names := []string{}
	for _, o := range items {
		names = append(names, o[key].(string))
	}
	return strings.Join(names, ",")
}

func TestProjectsListsMembershipsOfMe(t *testing.T) {
	f := &fakeAPI{
		objects: map[string]string{"users/me": `{"id":5,"username":"admin"}`},
		lists: map[string]string{"projects": `[
			{"id":1,"slug":"infra-2025","name":"Infra 2025","i_am_member":true,"logo_small_url":"http://t/media/logo.png?token=SECRET","my_permissions":["view_us"],"userstories_csv_uuid":"SECRET-uuid","transfer_token":"SECRET-transfer"},
			{"id":2,"slug":"outro","name":"Projeto Ágil","i_am_member":true},
			{"id":3,"slug":"public","name":"Público","i_am_member":false}]`},
	}
	all, err := Projects(context.Background(), f, "")
	if err != nil {
		t.Fatal(err)
	}
	if catalogNames(t, all, "slug") != "infra-2025,outro" || f.queries[0].Get("member") != "5" {
		t.Fatalf("%v %v", all, f.queries)
	}
	if b, _ := json.Marshal(all); strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), "my_permissions") {
		t.Fatalf("signed URL must be redacted, other keys kept: %s", b)
	}
	for search, want := range map[string]string{"INFRA": "infra-2025", "ágil": "outro", "agil": "", "outro": "outro"} {
		got, err := Projects(context.Background(), f, search)
		if err != nil {
			t.Fatal(err)
		}
		if names := catalogNames(t, got, "slug"); names != want {
			t.Errorf("search %q: got %q want %q", search, names, want)
		}
	}
	for _, q := range f.queries {
		if q.Has("q") {
			t.Fatalf("the server full-text search is never sent: %v", q)
		}
	}
}

func TestUsersAreMembersWithRole(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{
		"users": `[{"id":5,"username":"admin","full_name":"Administrador","photo":"http://t/media/p.png?token=SECRET"},
			{"id":6,"username":"svc","full_name":"Conta de Serviço"},
			{"id":9,"username":"outsider","full_name":"Fora"}]`,
		"memberships": `[{"user":5,"is_admin":true,"role_name":"Product Owner","project":37},
			{"user":6,"is_admin":false,"role_name":"Back","project":37},
			{"user":null,"is_admin":false,"role_name":"Back","project":37,"email":"pending@example.com"}]`,
	}}
	s := service(t, f)
	users, err := s.Users(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if catalogNames(t, users, "username") != "admin,svc" {
		t.Fatalf("%v", users)
	}
	if users[0]["is_admin"] != true || users[0]["role_name"] != "Product Owner" || users[1]["is_admin"] != false {
		t.Fatalf("%v", users)
	}
	if b, _ := json.Marshal(users); strings.Contains(string(b), "SECRET") {
		t.Fatalf("%s", b)
	}
	for search, want := range map[string]string{"SVC": "svc", "serviço": "svc", "SERVIÇO": "svc", "servico": "", "fora": "", "adm": "admin"} {
		got, err := s.Users(context.Background(), search)
		if err != nil {
			t.Fatal(err)
		}
		if names := catalogNames(t, got, "username"); names != want {
			t.Errorf("search %q: got %q want %q", search, names, want)
		}
	}
}

func TestMilestonesFilterLocally(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{"milestones": `[
		{"id":1,"name":"Sprint 1","closed":true,"project":37},
		{"id":2,"name":"Sprint 2","closed":false,"project":37},
		{"id":3,"name":"Foreign","closed":false,"project":38}]`}}
	s := service(t, f)
	yes, no := true, false
	for _, tc := range []struct {
		search string
		closed *bool
		want   string
	}{
		{"", nil, "Sprint 1,Sprint 2"},
		{"", &yes, "Sprint 1"},
		{"", &no, "Sprint 2"},
		{"sprint 2", nil, "Sprint 2"},
		{"2", &yes, ""},
	} {
		f.queries = nil
		got, err := s.Milestones(context.Background(), tc.search, tc.closed)
		if err != nil {
			t.Fatal(err)
		}
		if names := catalogNames(t, got, "name"); names != tc.want {
			t.Errorf("%q %v: got %q want %q", tc.search, tc.closed, names, tc.want)
		}
		if tc.closed != nil && f.queries[0].Get("closed") != map[bool]string{true: "true", false: "false"}[*tc.closed] {
			t.Errorf("closed not sent: %v", f.queries)
		}
		if f.queries[0].Get("project") != "37" {
			t.Errorf("project not sent: %v", f.queries)
		}
	}
}

func epicFake() *fakeAPI {
	return &fakeAPI{
		objects: map[string]string{
			"epics/by_ref": `{"id":90,"ref":9,"project":37,"subject":"Paridade","description":"texto","version":3,
				"tags":[["cli","#fff"],["go",null]],"status":1,"is_closed":false,"color":"#abc",
				"owner_extra_info":{"photo":"http://t/media/u.png?token=SECRET"}}`,
		},
		lists: map[string]string{
			"epics": `[{"id":90,"ref":9,"project":37,"subject":"Paridade","tags":[["cli",null]],"is_closed":false},
				{"id":91,"ref":10,"project":37,"subject":"Fechado","tags":[],"is_closed":true}]`,
			"epics/90/related_userstories": `[{"user_story":6808,"epic":90,"order":2},{"user_story":7001,"epic":90,"order":1},{"user_story":555,"epic":90,"order":3}]`,
			"userstories": `[{"id":6808,"ref":246,"project":37,"subject":"Stories","project_extra_info":{"slug":"infra"}},
				{"id":7001,"ref":12,"project":38,"subject":"Outro","project_extra_info":{"slug":"outro"}},
				{"id":6809,"ref":247,"project":37,"subject":"not linked"}]`,
		},
	}
}

func TestEpicsListFiltersAndBuildsURL(t *testing.T) {
	f := epicFake()
	s := service(t, f)
	yes := true
	all, err := s.Epics(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalogNames(t, all, "subject") != "Paridade,Fechado" || all[0]["url"] != "http://taiga.test/project/infra/epic/9" {
		t.Fatalf("%v", all)
	}
	if tags, _ := json.Marshal(all[0]["tags"]); string(tags) != `["cli"]` {
		t.Fatalf("tags %s", tags)
	}
	closed, err := s.Epics(context.Background(), "", &yes)
	if err != nil || catalogNames(t, closed, "subject") != "Fechado" || f.queries[len(f.queries)-1].Get("status__is_closed") != "true" {
		t.Fatalf("%v %v %v", closed, err, f.queries)
	}
	found, err := s.Epics(context.Background(), "PARID", nil)
	if err != nil || catalogNames(t, found, "subject") != "Paridade" {
		t.Fatalf("%v %v", found, err)
	}
}

func TestEpicGetListsLinkedStories(t *testing.T) {
	f := epicFake()
	s := service(t, f)
	e, err := s.EpicDetail(context.Background(), "9")
	if err != nil {
		t.Fatal(err)
	}
	if e["description"] != "texto" || e["url"] != "http://taiga.test/project/infra/epic/9" {
		t.Fatalf("%v", e)
	}
	b, _ := json.Marshal(e["user_stories"])
	// Related order: 7001 (order 1), 6808 (2), then 555, which the account cannot read.
	if string(b) != `[{"id":7001,"project":38,"project_slug":"outro","ref":12,"subject":"Outro"},{"id":6808,"project":37,"ref":246,"subject":"Stories"},{"id":555}]` {
		t.Fatalf("user_stories %s", b)
	}
	if all, _ := json.Marshal(e); strings.Contains(string(all), "SECRET") {
		t.Fatalf("%s", all)
	}
	if _, err := s.EpicDetail(context.Background(), "x"); exitOf(err) != 2 {
		t.Fatalf("bad ref: %v", err)
	}
}

func TestEpicsRefusedWhenModuleIsOff(t *testing.T) {
	f := epicFake()
	f.objects["projects/by_slug"] = `{"id":37,"slug":"infra","is_epics_activated":false}`
	s, err := New(context.Background(), f, "infra")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error { _, err := s.Epics(context.Background(), "", nil); return err },
		func() error { _, err := s.EpicDetail(context.Background(), "9"); return err },
	} {
		err := call()
		if exitOf(err) != 5 || !strings.Contains(err.Error(), "epics module is disabled") {
			t.Fatalf("%v", err)
		}
	}
	if len(f.queries) != 0 {
		t.Fatalf("read epics with the module off: %v", f.queries)
	}
}
