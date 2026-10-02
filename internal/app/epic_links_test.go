package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// linkAPI is a Taiga with story 6808 (#246) of project 37 and epics 11, 12, 13 (#1, #2, #3).
// links holds the story's epics as the server sees them; post and del decide each answer and
// may change links, like a concurrent writer would.
type linkAPI struct {
	fakeAPI
	links    map[int64]bool
	log      []string
	post     func(f *linkAPI, epic int64) (*taiga.Response, error)
	del      func(f *linkAPI, epic int64) (*taiga.Response, error)
	reads    int
	readErr  func(n int) error // fails the n-th story read when it returns an error
	onRead   func(f *linkAPI, n int)
	created  bool
	storyRef int64
	// readsBeforeRemove is the story read on which a test's onRead acts.
	readsBeforeRemove int
}

var epicRefs = map[int64]int64{11: 1, 12: 2, 13: 3}

func newLinkAPI(t *testing.T, links ...int64) (*linkAPI, *Service) {
	t.Helper()
	f := &linkAPI{links: map[int64]bool{}, storyRef: 246}
	for _, l := range links {
		f.links[l] = true
	}
	f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		if f.links[epic] {
			return nil, &taiga.APIError{Status: 400, Method: "POST", Path: linkPath(epic), Body: []byte(`{"__all__": ["Related user story with this User story and Epic already exists."]}`)}
		}
		f.links[epic] = true
		return &taiga.Response{Status: 201, Body: []byte(fmt.Sprintf(`{"epic": %d, "user_story": 6808, "order": 1}`, epic))}, nil
	}
	f.del = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		if !f.links[epic] {
			return nil, &taiga.APIError{Status: 404, Method: "DELETE", Path: unlinkPath(epic, 6808), Body: []byte(`{"_error_message": ""}`)}
		}
		delete(f.links, epic)
		return &taiga.Response{Status: 204}, nil
	}
	f.lists = map[string]string{"epics": `[{"id":11,"ref":1,"project":37},{"id":12,"ref":2,"project":37},{"id":13,"ref":3,"project":37},{"id":99,"ref":4,"project":38}]`}
	s := service(t, &f.fakeAPI)
	s.API = f
	return f, s
}

func (f *linkAPI) storyJSON() string {
	ids := []int64{}
	for id := range f.links {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	epics := []map[string]any{}
	for _, id := range ids {
		epics = append(epics, map[string]any{"id": id, "ref": epicRefs[id], "subject": "E", "project": map[string]any{"id": 37, "slug": "infra"}})
	}
	b, _ := json.Marshal(map[string]any{"id": 6808, "ref": f.storyRef, "project": 37, "version": 7, "subject": "S", "tags": []any{}, "epics": epics})
	return string(b)
}

func (f *linkAPI) Do(ctx context.Context, r taiga.Request) (*taiga.Response, error) {
	switch {
	case r.Method == "GET" && (r.Path == "userstories/6808" || r.Path == "userstories/by_ref"):
		f.reads++
		if f.onRead != nil {
			f.onRead(f, f.reads)
		}
		if f.readErr != nil {
			if err := f.readErr(f.reads); err != nil {
				return nil, err
			}
		}
		return &taiga.Response{Status: 200, Body: []byte(f.storyJSON())}, nil
	case r.Method == "POST" && r.Path == "userstories":
		f.log = append(f.log, "POST userstories")
		f.created = true
		return &taiga.Response{Status: 201, Body: []byte(f.storyJSON())}, nil
	case r.Method == "POST":
		f.log = append(f.log, "POST "+r.Path)
		body := r.Body.(map[string]any)
		var epic int64
		if _, err := fmt.Sscanf(r.Path, "epics/%d/related_userstories", &epic); err != nil || body["epic"] != epic || body["user_story"] != int64(6808) {
			return nil, fmt.Errorf("unexpected POST %s %v", r.Path, body)
		}
		return f.post(f, epic)
	case r.Method == "DELETE":
		f.log = append(f.log, "DELETE "+r.Path)
		var epic, story int64
		if _, err := fmt.Sscanf(r.Path, "epics/%d/related_userstories/%d", &epic, &story); err != nil || story != 6808 {
			return nil, fmt.Errorf("unexpected DELETE %s", r.Path)
		}
		return f.del(f, epic)
	case r.Method != "GET":
		f.log = append(f.log, r.Method+" "+r.Path)
		return nil, fmt.Errorf("unexpected %s %s", r.Method, r.Path)
	}
	return f.fakeAPI.Do(ctx, r)
}

func (f *linkAPI) writes() string { return strings.Join(f.log, "; ") }

func linkStory(t *testing.T, s *Service) Object { return story(t, s) }

func epicRef(t *testing.T, s *Service, ref string) Object {
	t.Helper()
	e, err := s.LinkableEpic(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func lostAnswer(path string) error {
	return &taiga.NetworkError{Method: "POST", Path: path, Err: errors.New("connection reset by peer")}
}

func TestLinkEpicAddsOnce(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	got, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.writes() != "POST epics/12/related_userstories" {
		t.Fatalf("writes: %s", f.writes())
	}
	o := got.(Object)
	if o["changed"] != true || o["linked"] != true || fmt.Sprint(o["epics"]) != "[1 2]" || fmt.Sprint(o["removed"]) != "[]" {
		t.Fatalf("result: %v", o)
	}
}

func TestLinkEpicAlreadyLinkedIsNoop(t *testing.T) {
	f, s := newLinkAPI(t, 11, 12)
	st := linkStory(t, s)
	reads := f.reads
	got, err := s.LinkEpic(context.Background(), st, epicRef(t, s, "2"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.writes() != "" || f.reads != reads {
		t.Fatalf("writes %q, reads %d → %d", f.writes(), reads, f.reads)
	}
	if o := got.(Object); o["changed"] != false || o["linked"] != false {
		t.Fatalf("result: %v", o)
	}
	// replace with the epic already the only one is a no-op too.
	f2, s2 := newLinkAPI(t, 12)
	if _, err := s2.LinkEpic(context.Background(), linkStory(t, s2), epicRef(t, s2, "2"), true, false); err != nil || f2.writes() != "" {
		t.Fatalf("replace no-op: %v %s", err, f2.writes())
	}
}

func TestLinkEpicDuplicate400IsSuccess(t *testing.T) {
	f, s := newLinkAPI(t)
	st := linkStory(t, s)
	f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		f.links[epic] = true // linked by someone else between our re-read and the POST
		return nil, &taiga.APIError{Status: 400, Method: "POST", Path: linkPath(epic), Body: []byte(`{"__all__": ["already exists"]}`)}
	}
	got, err := s.LinkEpic(context.Background(), st, epicRef(t, s, "1"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.(Object)["epics"]) != "[1]" {
		t.Fatalf("result: %v", got)
	}
}

func TestLinkEpicOther400IsTheAPIError(t *testing.T) {
	f, s := newLinkAPI(t)
	f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 400, Method: "POST", Path: linkPath(epic), Body: []byte(`{"user_story": ["invalid"]}`)}
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false)
	if codeOf(err) != "invalid_request" || exitOf(err) != output.ExitUsage {
		t.Fatalf("err: %v", err)
	}
}

func TestLinkEpicLostAnswerLinked(t *testing.T) {
	f, s := newLinkAPI(t)
	f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		f.links[epic] = true
		return nil, lostAnswer(linkPath(epic))
	}
	if _, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false); err != nil {
		t.Fatal(err)
	}
	// A 2xx whose body does not decode is checked the same way.
	f2, s2 := newLinkAPI(t)
	f2.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		f.links[epic] = true
		return &taiga.Response{Status: 201, Body: []byte("{")}, nil
	}
	if _, err := s2.LinkEpic(context.Background(), linkStory(t, s2), epicRef(t, s2, "1"), false, false); err != nil {
		t.Fatal(err)
	}
}

func TestLinkEpicLostAnswerNotLinked(t *testing.T) {
	for name, answer := range map[string]error{
		"network": lostAnswer(linkPath(11)),
		"5xx":     &taiga.APIError{Status: 502, Method: "POST", Path: linkPath(11)},
	} {
		t.Run(name, func(t *testing.T) {
			f, s := newLinkAPI(t)
			f.post = func(*linkAPI, int64) (*taiga.Response, error) { return nil, answer }
			_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false)
			if codeOf(err) != "epic_link_unconfirmed" || exitOf(err) != output.ExitUnexpected {
				t.Fatalf("err: %v (exit %d)", err, exitOf(err))
			}
			if !strings.Contains(output.AsError(err).Recovery, "run the same command again") || strings.Count(f.writes(), "POST") != 1 {
				t.Fatalf("recovery %q, writes %s", output.AsError(err).Recovery, f.writes())
			}
		})
	}
	// The check read fails too: still unconfirmed, never a network exit.
	f, s := newLinkAPI(t)
	st := linkStory(t, s)
	f.post = func(*linkAPI, int64) (*taiga.Response, error) { return nil, lostAnswer(linkPath(11)) }
	f.readErr = func(n int) error {
		if n >= 3 {
			return &taiga.NetworkError{Method: "GET", Path: "userstories/6808", Err: errors.New("timeout")}
		}
		return nil
	}
	_, err := s.LinkEpic(context.Background(), st, epicRef(t, s, "1"), false, false)
	if codeOf(err) != "epic_link_unconfirmed" || exitOf(err) != output.ExitUnexpected {
		t.Fatalf("err: %v (exit %d)", err, exitOf(err))
	}
}

func TestLinkEpicNotSentIsNetworkError(t *testing.T) {
	f, s := newLinkAPI(t)
	f.post = func(*linkAPI, int64) (*taiga.Response, error) {
		return nil, &taiga.NetworkError{Method: "POST", Path: linkPath(11), Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false)
	if codeOf(err) != "network_error" || exitOf(err) != output.ExitNetwork {
		t.Fatalf("err: %v", err)
	}
}

func TestLinkEpicForbiddenIsForbidden(t *testing.T) {
	f, s := newLinkAPI(t)
	f.post = func(*linkAPI, int64) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 403, Method: "POST", Path: linkPath(11), Body: []byte(`{"_error_message": "You do not have permission to perform this action."}`)}
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false)
	if codeOf(err) != "forbidden" || exitOf(err) != output.ExitForbidden {
		t.Fatalf("err: %v", err)
	}
}

func TestLinkEpicRefusesChangedLinksBeforePost(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	st := linkStory(t, s)
	f.links[13] = true // someone linked epic #3 after our read
	_, err := s.LinkEpic(context.Background(), st, epicRef(t, s, "2"), true, false)
	if codeOf(err) != "version_conflict" || exitOf(err) != output.ExitConflict || f.writes() != "" {
		t.Fatalf("err %v, writes %s", err, f.writes())
	}
}

func TestReplaceEpicPostsBeforeDelete(t *testing.T) {
	f, s := newLinkAPI(t, 11, 13)
	got, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "POST epics/12/related_userstories; DELETE epics/11/related_userstories/6808; DELETE epics/13/related_userstories/6808"
	if f.writes() != want {
		t.Fatalf("writes:\n%s\nwant:\n%s", f.writes(), want)
	}
	o := got.(Object)
	if fmt.Sprint(o["epics"]) != "[2]" || fmt.Sprint(o["removed"]) != "[1 3]" || o["linked"] != true {
		t.Fatalf("result: %v", o)
	}
}

func TestReplaceEpicDryRunListsEveryRequest(t *testing.T) {
	f, s := newLinkAPI(t, 11, 13)
	got, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, true)
	if err != nil || f.writes() != "" {
		t.Fatalf("err %v, writes %s", err, f.writes())
	}
	b, _ := json.Marshal(got)
	want := `{"dry_run":true,"requests":[{"dry_run":true,"method":"POST","path":"epics/12/related_userstories","body":{"epic":12,"user_story":6808}},` +
		`{"dry_run":true,"method":"DELETE","path":"epics/11/related_userstories/6808","body":null},{"dry_run":true,"method":"DELETE","path":"epics/13/related_userstories/6808","body":null}]}`
	if string(b) != want {
		t.Fatalf("plan:\n%s\nwant:\n%s", b, want)
	}
}

func TestReplaceEpicStopsOnNewForeignLink(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		f.links[epic], f.links[13] = true, true // epic #3 linked by someone else right after our POST
		return &taiga.Response{Status: 201, Body: []byte(fmt.Sprintf(`{"epic": %d, "user_story": 6808}`, epic))}, nil
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	if codeOf(err) != "epic_links_postcondition_failed" || exitOf(err) != output.ExitConflict {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(f.writes(), "DELETE") {
		t.Fatalf("deleted: %s", f.writes())
	}
	if c := output.AsError(err).Cause; !strings.Contains(c, "saved") || !strings.Contains(c, "3") {
		t.Fatalf("cause: %s", c)
	}
}

func TestReplaceEpicDelete404IsDone(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	f.del = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		delete(f.links, epic) // removed by someone else just before our DELETE
		return nil, &taiga.APIError{Status: 404, Method: "DELETE", Path: unlinkPath(epic, 6808)}
	}
	got, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.(Object)["epics"]) != "[2]" {
		t.Fatalf("result: %v", got)
	}
}

func TestReplaceEpicDeleteFailsIsIncomplete(t *testing.T) {
	f, s := newLinkAPI(t, 11, 13)
	f.del = func(f *linkAPI, epic int64) (*taiga.Response, error) {
		if epic == 13 {
			return nil, &taiga.NetworkError{Method: "DELETE", Path: unlinkPath(epic, 6808), Err: errors.New("connection reset by peer")}
		}
		delete(f.links, epic)
		return &taiga.Response{Status: 204}, nil
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	if codeOf(err) != "epic_replace_incomplete" || exitOf(err) != output.ExitUnexpected {
		t.Fatalf("err: %v (exit %d)", err, exitOf(err))
	}
	e := output.AsError(err)
	if !strings.Contains(e.Cause, "linked to epic #2") || !strings.Contains(e.Cause, "removed [1]") || !strings.Contains(e.Cause, "remaining [3]") {
		t.Fatalf("cause: %s", e.Cause)
	}
	if !strings.Contains(e.Recovery, "run the same command again") || strings.Count(f.writes(), "DELETE epics/13") != 1 {
		t.Fatalf("recovery %q, writes %s", e.Recovery, f.writes())
	}
}

func TestReplaceEpicRerunConverges(t *testing.T) {
	// A first run linked #2 and could not remove #1: the second run only removes #1.
	f, s := newLinkAPI(t, 11, 12)
	got, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.writes() != "DELETE epics/11/related_userstories/6808" {
		t.Fatalf("writes: %s", f.writes())
	}
	if o := got.(Object); fmt.Sprint(o["epics"]) != "[2]" || o["linked"] != false || o["changed"] != true {
		t.Fatalf("result: %v", o)
	}
}

func TestStoryCreateEpicLinkFailureNamesTheStory(t *testing.T) {
	f, s := newLinkAPI(t)
	f.storyRef = 300
	f.post = func(*linkAPI, int64) (*taiga.Response, error) { return nil, lostAnswer(linkPath(11)) }
	_, err := s.CreateStoryWithEpic(context.Background(), Object{"subject": "S"}, epicRef(t, s, "1"), false)
	if !f.created {
		t.Fatal("story not created")
	}
	e := output.AsError(err)
	if e.Code != "story_created_link_failed" || e.Exit != output.ExitUnexpected {
		t.Fatalf("err: %v (exit %d)", err, e.Exit)
	}
	if !strings.Contains(e.Cause, "#300") || !strings.Contains(e.Recovery, "taiga epic link 1 300") ||
		!strings.Contains(e.Recovery, "do not run the create command again") || strings.Contains(e.Recovery, "run the same command") {
		t.Fatalf("cause %q recovery %q", e.Cause, e.Recovery)
	}
	if strings.Count(f.writes(), "POST userstories") != 1 {
		t.Fatalf("writes: %s", f.writes())
	}
}

func TestStoryCreateWithEpicLinks(t *testing.T) {
	f, s := newLinkAPI(t)
	got, err := s.CreateStoryWithEpic(context.Background(), Object{"subject": "S"}, epicRef(t, s, "1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if f.writes() != "POST userstories; POST epics/11/related_userstories" {
		t.Fatalf("writes: %s", f.writes())
	}
	if !strings.Contains(fmt.Sprint(got.(Object)["epics"]), "id:11") {
		t.Fatalf("story: %v", got)
	}
}

func TestStoryCreateWithEpicDryRun(t *testing.T) {
	f, s := newLinkAPI(t)
	got, err := s.CreateStoryWithEpic(context.Background(), Object{"subject": "S"}, epicRef(t, s, "1"), true)
	if err != nil || f.writes() != "" {
		t.Fatalf("err %v writes %s", err, f.writes())
	}
	plan := got.(LinkPlan)
	if len(plan.Requests) != 2 || plan.Requests[0].Path != "userstories" || plan.Requests[1].Path != "epics/11/related_userstories" {
		t.Fatalf("plan: %+v", plan)
	}
}

func TestStoryUpdateLinkFailureSaysFieldsSaved(t *testing.T) {
	f, s := newLinkAPI(t)
	f.objects["userstories/6808"] = f.storyJSON()
	patched := false
	f.post = func(*linkAPI, int64) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 403, Method: "POST", Path: linkPath(11)}
	}
	api := &patchingAPI{linkAPI: f, patched: &patched}
	s.API = api
	_, err := s.UpdateStoryWithEpic(context.Background(), "246", Patch{Set: Object{"subject": "new"}}, epicRef(t, s, "1"), false, false, false)
	e := output.AsError(err)
	if !patched || e.Code != "story_updated_link_failed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Cause, "were saved") || !strings.Contains(e.Cause, "forbidden") || !strings.Contains(e.Recovery, "taiga epic link 1 246") ||
		!strings.Contains(e.Recovery, "do not run the update command again") {
		t.Fatalf("patched %v err %v recovery %q", patched, err, e.Recovery)
	}
}

// patchingAPI accepts the story PATCH of an update (sent through WriteVersionedFrom).
type patchingAPI struct {
	*linkAPI
	patched *bool
}

func (p *patchingAPI) WriteVersionedFrom(_ context.Context, method, path string, _ map[string]any, _ map[string]json.RawMessage, _ bool) (*taiga.Response, error) {
	if method != "PATCH" || path != "userstories/6808" {
		return nil, fmt.Errorf("unexpected %s %s", method, path)
	}
	*p.patched = true
	return &taiga.Response{Status: 200, Body: []byte(p.storyJSON())}, nil
}

func TestEpicFromOtherProjectIsNotFound(t *testing.T) {
	_, s := newLinkAPI(t)
	if _, err := s.LinkableEpic(context.Background(), "4"); codeOf(err) != "not_found" {
		t.Fatalf("err: %v", err)
	}
}

func TestLinkableEpicRefusesDisabledModule(t *testing.T) {
	_, s := newLinkAPI(t)
	s.Project["is_epics_activated"] = false
	if _, err := s.LinkableEpic(context.Background(), "1"); codeOf(err) != "not_found" || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err: %v", err)
	}
}

func TestLinkEpicPostconditionFails(t *testing.T) {
	f, s := newLinkAPI(t)
	f.post = func(_ *linkAPI, epic int64) (*taiga.Response, error) {
		// Taiga answers the link but the story does not show it.
		return &taiga.Response{Status: 201, Body: []byte(fmt.Sprintf(`{"epic": %d, "user_story": 6808}`, epic))}, nil
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "1"), false, false)
	if codeOf(err) != "epic_links_postcondition_failed" || exitOf(err) != output.ExitConflict {
		t.Fatalf("err: %v", err)
	}
	// A DELETE answered 204 that left the old link: replace is not done.
	f2, s2 := newLinkAPI(t, 11)
	f2.del = func(*linkAPI, int64) (*taiga.Response, error) { return &taiga.Response{Status: 204}, nil }
	_, err = s2.LinkEpic(context.Background(), linkStory(t, s2), epicRef(t, s2, "2"), true, false)
	if codeOf(err) != "epic_links_postcondition_failed" {
		t.Fatalf("err: %v", err)
	}
}

func TestReplaceEpicStopsWhenNewLinkVanishes(t *testing.T) {
	for name, links := range map[string][]int64{"with POST": {11}, "rerun": {11, 12}} {
		t.Run(name, func(t *testing.T) {
			f, s := newLinkAPI(t, links...)
			st := linkStory(t, s)
			// The read in removeOld comes after the pre-check (and the POST): someone unlinks #2 in between.
			f.onRead = func(f *linkAPI, n int) {
				if n == f.readsBeforeRemove {
					delete(f.links, 12)
				}
			}
			f.readsBeforeRemove = f.reads + 2
			_, err := s.LinkEpic(context.Background(), st, epicRef(t, s, "2"), true, false)
			if codeOf(err) != "epic_links_postcondition_failed" || strings.Contains(f.writes(), "DELETE") {
				t.Fatalf("err %v, writes %s", err, f.writes())
			}
			if c := output.AsError(err).Cause; strings.Contains(c, "is saved") || !strings.Contains(c, "not among") {
				t.Fatalf("cause: %s", c)
			}
		})
	}
}

func TestStoryUpdateLinkNetworkFailureAfterPatchIsNotExit7(t *testing.T) {
	for name, setup := range map[string]func(f *linkAPI){
		"post not sent": func(f *linkAPI) {
			f.post = func(*linkAPI, int64) (*taiga.Response, error) {
				return nil, &taiga.NetworkError{Method: "POST", Path: linkPath(11), Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}
			}
		},
		"pre-read fails": func(f *linkAPI) {
			f.readErr = func(n int) error {
				if n == 3 { // 1: the update's read; 2: the re-read after the PATCH; 3: the re-read before the POST
					return &taiga.APIError{Status: 502, Method: "GET", Path: "userstories/6808"}
				}
				return nil
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, s := newLinkAPI(t)
			patched := false
			setup(f)
			s.API = &patchingAPI{linkAPI: f, patched: &patched}
			_, err := s.UpdateStoryWithEpic(context.Background(), "246", Patch{Set: Object{"subject": "new"}}, epicRef(t, s, "1"), false, false, false)
			if !patched || codeOf(err) != "story_updated_link_failed" || exitOf(err) != output.ExitUnexpected {
				t.Fatalf("patched %v err %v exit %d", patched, err, exitOf(err))
			}
		})
	}
}

func TestStoryUpdateLinkConflictAfterPatchIsNotRetryable(t *testing.T) {
	f, s := newLinkAPI(t)
	patched := false
	f.readErr = func(n int) error {
		if n == 3 {
			f.links[13] = true // linked by someone else after the PATCH
		}
		return nil
	}
	s.API = &patchingAPI{linkAPI: f, patched: &patched}
	_, err := s.UpdateStoryWithEpic(context.Background(), "246", Patch{Set: Object{"subject": "new"}}, epicRef(t, s, "1"), false, false, false)
	if !patched || codeOf(err) != "story_updated_link_failed" || exitOf(err) != output.ExitUnexpected || !strings.Contains(err.Error(), "version_conflict") {
		t.Fatalf("patched %v err %v exit %d", patched, err, exitOf(err))
	}
}

func TestStoryUpdateNoopLinkUsesThePatchResult(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	patched := false
	f.readErr = func(n int) error {
		if n >= 3 {
			return &taiga.NetworkError{Method: "GET", Path: "userstories/6808", Err: errors.New("i/o timeout")}
		}
		return nil
	}
	s.API = &patchingAPI{linkAPI: f, patched: &patched}
	got, err := s.UpdateStoryWithEpic(context.Background(), "246", Patch{Set: Object{"subject": "new"}}, epicRef(t, s, "1"), false, false, false)
	if err != nil || !patched || ID(got.(Object)["id"]) != 6808 {
		t.Fatalf("patched %v got %v err %v", patched, got, err)
	}
}

func TestReplaceEpicDeleteRefusedDoesNotPromiseConvergence(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	f.del = func(*linkAPI, int64) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 403, Method: "DELETE", Path: unlinkPath(11, 6808), Body: []byte(`{"_error_message": "You do not have permission to perform this action."}`)}
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	e := output.AsError(err)
	if e.Code != "epic_replace_incomplete" || strings.Contains(e.Recovery, "run the same command again") || !strings.Contains(e.Recovery, "permission") ||
		strings.Contains(e.Cause, "may or may not") {
		t.Fatalf("err %v recovery %q", err, e.Recovery)
	}
}

// updateWithReplace runs story update --subject new --replace-epic #2 on a story with epic #1,
// with setup changing the fake first.
func updateWithReplace(t *testing.T, setup func(f *linkAPI)) *output.Error {
	t.Helper()
	f, s := newLinkAPI(t, 11)
	patched := false
	setup(f)
	s.API = &patchingAPI{linkAPI: f, patched: &patched}
	_, err := s.UpdateStoryWithEpic(context.Background(), "246", Patch{Set: Object{"subject": "new"}}, epicRef(t, s, "2"), true, false, false)
	e := output.AsError(err)
	if !patched || e.Code != "story_updated_link_failed" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Recovery, "do not run the update command again") {
		t.Fatalf("patched %v err %v", patched, err)
	}
	return e
}

func TestStoryUpdateConflictKeepsTheInspectRecovery(t *testing.T) {
	e := updateWithReplace(t, func(f *linkAPI) {
		inner := f.post
		f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
			f.links[13] = true // linked by someone else between the POST and the removal
			return inner(f, epic)
		}
	})
	if !strings.Contains(e.Cause, "epic_links_postcondition_failed") || !strings.Contains(e.Recovery, "taiga story get") ||
		strings.Contains(e.Recovery, "--replace --confirm-delete`") {
		t.Fatalf("cause %q recovery %q", e.Cause, e.Recovery)
	}
}

func TestStoryUpdateEpicsChangedBeforePostKeepsTheInspectRecovery(t *testing.T) {
	e := updateWithReplace(t, func(f *linkAPI) {
		f.onRead = func(f *linkAPI, n int) {
			if n == 3 { // the re-read before the POST, after the PATCH
				f.links[13] = true
			}
		}
	})
	if !strings.Contains(e.Cause, "version_conflict") || !strings.Contains(e.Recovery, "check them with `taiga story get 246` and decide before") {
		t.Fatalf("cause %q recovery %q", e.Cause, e.Recovery)
	}
}

func TestStoryUpdateDeleteRefusedKeepsThePermissionRecovery(t *testing.T) {
	e := updateWithReplace(t, func(f *linkAPI) {
		f.del = func(*linkAPI, int64) (*taiga.Response, error) {
			return nil, &taiga.APIError{Status: 403, Method: "DELETE", Path: unlinkPath(11, 6808)}
		}
	})
	if !strings.Contains(e.Cause, "epic_replace_incomplete") || !strings.Contains(e.Recovery, "modify_epic") || strings.Contains(e.Recovery, "converges") {
		t.Fatalf("cause %q recovery %q", e.Cause, e.Recovery)
	}
}

func TestStoryUpdateUncertainLinkPointsToEpicLink(t *testing.T) {
	e := updateWithReplace(t, func(f *linkAPI) {
		f.post = func(*linkAPI, int64) (*taiga.Response, error) { return nil, lostAnswer(linkPath(12)) }
	})
	if !strings.Contains(e.Recovery, "taiga epic link 2 246 --replace --confirm-delete") {
		t.Fatalf("recovery %q", e.Recovery)
	}
}

func TestLinkEpicPostRedirectIsUncertain(t *testing.T) {
	for name, applied := range map[string]bool{"applied": true, "not applied": false} {
		t.Run(name, func(t *testing.T) {
			f, s := newLinkAPI(t, 11)
			f.post = func(f *linkAPI, epic int64) (*taiga.Response, error) {
				if applied {
					f.links[epic] = true
				}
				return nil, &taiga.APIError{Status: 302, Method: "POST", Path: linkPath(epic)}
			}
			_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
			if exitOf(err) == output.ExitNetwork {
				t.Fatalf("exit 7 after the POST was sent: %v", err)
			}
			if applied && err != nil || !applied && codeOf(err) != "epic_link_unconfirmed" {
				t.Fatalf("err: %v", err)
			}
		})
	}
}

func TestReplaceEpicDeleteRedirectIsNotExit7(t *testing.T) {
	f, s := newLinkAPI(t, 11)
	f.del = func(*linkAPI, int64) (*taiga.Response, error) {
		return nil, &taiga.APIError{Status: 307, Method: "DELETE", Path: unlinkPath(11, 6808)}
	}
	_, err := s.LinkEpic(context.Background(), linkStory(t, s), epicRef(t, s, "2"), true, false)
	e := output.AsError(err)
	if e.Code != "epic_replace_incomplete" || e.Exit != output.ExitUnexpected || !strings.Contains(e.Recovery, "run the same command again") {
		t.Fatalf("err %v recovery %q", err, e.Recovery)
	}
}

func TestStoryCreateWithEpicAlreadyLinkedReturnsTheStory(t *testing.T) {
	f, s := newLinkAPI(t)
	f.storyRef = 300
	f.onRead = func(f *linkAPI, n int) { f.links[11] = true } // linked by another process before the re-read
	got, err := s.CreateStoryWithEpic(context.Background(), Object{"subject": "S"}, epicRef(t, s, "1"), false)
	if err != nil {
		t.Fatal(err)
	}
	o := got.(Object)
	if ID(o["id"]) != 6808 || ID(o["ref"]) != 300 || !strings.HasSuffix(fmt.Sprint(o["url"]), "/us/300") || strings.Contains(f.writes(), "related_userstories") {
		t.Fatalf("story %v writes %s", o, f.writes())
	}
}
