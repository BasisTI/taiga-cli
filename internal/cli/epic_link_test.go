package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// handleEpicLinks serves POST epics/<id>/related_userstories and DELETE
// epics/<id>/related_userstories/<story id> on the stories' "epics", with Taiga's answers: 400 on a
// duplicate, 404 on a link that is not there (docs/api-notes.md).
func (f *storyFake) handleEpicLinks(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "epics" || parts[2] != "related_userstories" {
		return false
	}
	epicID, _ := strconv.ParseInt(parts[1], 10, 64)
	var epic map[string]any
	for _, e := range f.epics {
		if fmt.Sprint(e["id"]) == fmt.Sprint(epicID) {
			epic = e
		}
	}
	linked := func(s map[string]any) (int, bool) {
		list, _ := s["epics"].([]any)
		for i, x := range list {
			if fmt.Sprint(x.(map[string]any)["id"]) == fmt.Sprint(epicID) {
				return i, true
			}
		}
		return -1, false
	}
	switch {
	case r.Method == "POST" && len(parts) == 3:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s := f.stories[int64(body["user_story"].(float64))]
		if epic == nil || s == nil || fmt.Sprint(body["epic"]) != parts[1] {
			f.t.Errorf("bad link POST %s %v", path, body)
			w.WriteHeader(400)
			return true
		}
		if _, ok := linked(s); ok {
			w.WriteHeader(400)
			_, _ = fmt.Fprint(w, `{"__all__": ["Related user story with this User story and Epic already exists."]}`)
			return true
		}
		list, _ := s["epics"].([]any)
		s["epics"] = append(list, map[string]any{"id": epicID, "ref": epic["ref"], "subject": epic["subject"], "project": map[string]any{"id": 37, "slug": f.slug}})
		w.WriteHeader(201)
		f.write(w, map[string]any{"epic": epicID, "user_story": body["user_story"], "order": 1})
	case r.Method == "DELETE" && len(parts) == 4:
		id, _ := strconv.ParseInt(parts[3], 10, 64)
		s := f.stories[id]
		i, ok := linked(s)
		if s == nil || !ok {
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, `{"_error_message": ""}`)
			return true
		}
		list := s["epics"].([]any)
		s["epics"] = append(list[:i:i], list[i+1:]...)
		w.WriteHeader(204)
	default:
		f.t.Errorf("unexpected %s %s", r.Method, path)
		w.WriteHeader(404)
	}
	return true
}

func writeLog(calls []recorded) string {
	out := []string{}
	for _, c := range calls {
		if c.method != "GET" {
			out = append(out, c.method+" "+strings.TrimPrefix(c.path, "/api/v1/"))
		}
	}
	return strings.Join(out, "; ")
}

func TestEpicLinkAddsAndIsIdempotent(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "epic", "link", "91", "246", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["changed"] != true || got["linked"] != true || fmt.Sprint(got["epics"]) != "[91 9]" || fmt.Sprint(got["story"]) != "246" {
		t.Fatalf("%s", out)
	}
	if writeLog(*calls) != "POST epics/9/related_userstories" {
		t.Fatalf("writes: %s", writeLog(*calls))
	}
	*calls = nil
	out, _, code = runIn(t, f.env(), "", "epic", "link", "91", "246", "--output", "text")
	if code != 0 || writeLog(*calls) != "" || !strings.Contains(out, "changed:  false") || !strings.Contains(out, "epics:    91, 9") {
		t.Fatalf("%d %s writes %s", code, out, writeLog(*calls))
	}
}

func TestReplaceEpicRequiresConfirmDelete(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
	for _, args := range [][]string{
		{"epic", "link", "91", "246", "--replace"},
		{"epic", "link", "91", "246", "--replace", "--dry-run"},
		{"story", "update", "246", "--replace-epic", "91"},
		{"story", "update", "246", "--replace-epic", "91", "--dry-run"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || !strings.Contains(stderr, "delete_not_confirmed") || !strings.Contains(stderr, "--confirm-delete") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	for _, args := range [][]string{
		{"epic", "link", "91", "246", "--confirm-delete"},
		{"epic", "link", "0", "246"},
		{"epic", "link", "91"},
		{"story", "update", "246", "--confirm-delete"},
		{"story", "update", "246", "--epic", "9", "--replace-epic", "91", "--confirm-delete"},
		{"story", "update", "246", "--epic", "abc"},
		{"story", "update", "246", "--replace-epic", "", "--confirm-delete"},
		{"story", "create", "--subject", "s", "--epic", "-1"},
		{"story", "create", "--subject", "s", "--replace-epic", "91"},
	} {
		_, stderr, code := runIn(t, env, "", args...)
		if code != 2 || strings.Contains(stderr, "network_error") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestEpicLinkReplacePostsBeforeDelete(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "epic", "link", "91", "246", "--replace", "--confirm-delete", "--dry-run", "--output", "json")
	if code != 0 || writeLog(*calls) != "" {
		t.Fatalf("%d %s writes %s", code, stderr, writeLog(*calls))
	}
	if !strings.Contains(out, `"method": "POST"`) || !strings.Contains(out, `"path": "epics/90/related_userstories/6808"`) ||
		strings.Index(out, `"POST"`) > strings.Index(out, `"DELETE"`) {
		t.Fatalf("plan: %s", out)
	}
	out, stderr, code = runIn(t, f.env(), "", "epic", "link", "91", "246", "--replace", "--confirm-delete", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if writeLog(*calls) != "POST epics/9/related_userstories; DELETE epics/90/related_userstories/6808" {
		t.Fatalf("writes: %s", writeLog(*calls))
	}
	if !strings.Contains(out, `"removed": [`) || !strings.Contains(out, `"9"`) {
		t.Fatalf("%s", out)
	}
	if e := f.stories[6808]["epics"].([]any); len(e) != 1 || fmt.Sprint(e[0].(map[string]any)["id"]) != "9" {
		t.Fatalf("epics: %v", e)
	}
}

func TestEpicLinkDeleteFailureThenRerunConverges(t *testing.T) {
	f, calls := newStoryFake(t)
	f.fail["DELETE epics/90/related_userstories/6808"] = 502
	_, stderr, code := runIn(t, f.env(), "", "epic", "link", "91", "246", "--replace", "--confirm-delete")
	if code != 1 || !strings.Contains(stderr, "epic_replace_incomplete") || !strings.Contains(stderr, "run the same command again") {
		t.Fatalf("%d %s", code, stderr)
	}
	delete(f.fail, "DELETE epics/90/related_userstories/6808")
	*calls = nil
	_, stderr, code = runIn(t, f.env(), "", "epic", "link", "91", "246", "--replace", "--confirm-delete")
	if code != 0 || writeLog(*calls) != "DELETE epics/90/related_userstories/6808" {
		t.Fatalf("%d %s writes %s", code, stderr, writeLog(*calls))
	}
}

func TestStoryUpdateReplaceEpicAfterFields(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--subject", "novo", "--replace-epic", "91", "--confirm-delete", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if writeLog(*calls) != "PATCH userstories/6808; POST epics/9/related_userstories; DELETE epics/90/related_userstories/6808" {
		t.Fatalf("writes: %s", writeLog(*calls))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["subject"] != "novo" || !strings.Contains(fmt.Sprint(got["epics"]), "ref:91") {
		t.Fatalf("%v %s", err, out)
	}
	*calls = nil
	out, _, code = runIn(t, f.env(), "", "story", "update", "246", "--subject", "outro", "--epic", "9", "--dry-run", "--output", "json")
	if code != 0 || writeLog(*calls) != "" || !strings.Contains(out, `"PATCH"`) || !strings.Contains(out, `"epics/90/related_userstories"`) {
		t.Fatalf("%d %s writes %s", code, out, writeLog(*calls))
	}
}

func TestStoryUpdateEpicLinkFailureAfterPatch(t *testing.T) {
	f, _ := newStoryFake(t)
	f.fail["POST epics/9/related_userstories"] = 403
	_, stderr, code := runIn(t, f.env(), "", "story", "update", "246", "--append-description", "x", "--epic", "91")
	if code != 1 || !strings.Contains(stderr, "story_updated_link_failed") || !strings.Contains(stderr, "[forbidden]") || !strings.Contains(stderr, "were saved") ||
		!strings.Contains(stderr, "taiga epic link 91 246") || !strings.Contains(stderr, "do not run the update command again") {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestStoryCreateWithEpic(t *testing.T) {
	f, calls := newStoryFake(t)
	out, stderr, code := runIn(t, f.env(), "", "story", "create", "--subject", "nova", "--epic", "91", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if writeLog(*calls) != "POST userstories; POST epics/9/related_userstories" || !strings.Contains(out, `"ref": 91`) {
		t.Fatalf("writes %s out %s", writeLog(*calls), out)
	}
	*calls = nil
	f.fail["POST epics/9/related_userstories"] = 500
	_, stderr, code = runIn(t, f.env(), "", "story", "create", "--subject", "outra", "--epic", "91")
	if code != 1 || !strings.Contains(stderr, "story_created_link_failed") || !strings.Contains(stderr, "#300") ||
		!strings.Contains(stderr, "taiga epic link 91 300") || strings.Contains(stderr, "run the same command") {
		t.Fatalf("%d %s", code, stderr)
	}
	if strings.Count(writeLog(*calls), "POST userstories") != 1 {
		t.Fatalf("writes: %s", writeLog(*calls))
	}
}

func TestEpicLinkUnknownEpicSendsNothing(t *testing.T) {
	f, calls := newStoryFake(t)
	for _, args := range [][]string{{"epic", "link", "77", "246"}, {"story", "create", "--subject", "s", "--epic", "77"}, {"story", "update", "246", "--subject", "x", "--epic", "77"}} {
		_, stderr, code := runIn(t, f.env(), "", args...)
		if code != 5 || !strings.Contains(stderr, "not_found") || writeLog(*calls) != "" {
			t.Fatalf("%v: %d %s writes %s", args, code, stderr, writeLog(*calls))
		}
	}
}

func TestEpicLinkPostRedirectIsNotExit7(t *testing.T) {
	f, calls := newStoryFake(t)
	f.fail["POST epics/9/related_userstories"] = 302
	_, stderr, code := runIn(t, f.env(), "", "epic", "link", "91", "246")
	if code != 1 || !strings.Contains(stderr, "epic_link_unconfirmed") || strings.Count(writeLog(*calls), "POST") != 1 {
		t.Fatalf("%d %s writes %s", code, stderr, writeLog(*calls))
	}
}
