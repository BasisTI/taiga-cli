//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// storyEpics returns the ids in the "epics" of the story, sorted, and its version.
func storyEpics(t *testing.T, c *Client, story int64) ([]int64, int64) {
	t.Helper()
	o := probeDo(t, c, "GET", fmt.Sprintf("userstories/%d", story), nil, nil)
	var epics []struct{ ID int64 }
	if string(o["epics"]) != "null" {
		if err := json.Unmarshal(o["epics"], &epics); err != nil {
			t.Fatalf("epics %s: %v", o["epics"], err)
		}
	}
	out := []int64{}
	for _, e := range epics {
		out = append(out, e.ID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, o.int("version")
}

func linkStatus(c *Client, method, path string, body any) (int, string) {
	resp, err := c.Do(context.Background(), Request{Method: method, Path: path, Body: body})
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			return ae.Status, string(ae.Body)
		}
		return 0, err.Error()
	}
	return resp.Status, string(resp.Body)
}

// TestProbeEpicLinkContract records the contract of PR 253-2: the story↔epic link is created by
// POST epics/<id>/related_userstories and removed by DELETE epics/<id>/related_userstories/<story>,
// without version; the server's unique (user_story, epic) turns a repeated POST into an error.
func TestProbeEpicLinkContract(t *testing.T) {
	c := probeClient(t)
	svc := svcClient(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	project := ensureProject(t, c, "cli-test-probe-epic-links-"+suffix)
	other := ensureProject(t, c, "cli-test-probe-epic-links-other-"+suffix)
	svcID := userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svcID, testtaiga.ServiceEmail)
	epic := func(p int64, s string) int64 {
		return probeDo(t, c, "POST", "epics", nil, map[string]any{"project": p, "subject": s}).int("id")
	}
	e1, e2, foreign := epic(project, "E1 "+suffix), epic(project, "E2 "+suffix), epic(other, "F "+suffix)
	story := createStory(t, c, project, "story "+suffix, nil).int("id")
	rel := func(e int64) string { return fmt.Sprintf("epics/%d/related_userstories", e) }
	del := func(e int64) string { return fmt.Sprintf("epics/%d/related_userstories/%d", e, story) }
	_, v0 := storyEpics(t, c, story)

	// 1. POST: 201, the answer shape, version untouched, epics re-read at once.
	status, body := linkStatus(c, "POST", rel(e1), map[string]any{"epic": e1, "user_story": story})
	t.Logf("FINDING POST link: %d %s", status, body)
	if status != 201 {
		t.Fatalf("POST link: %d", status)
	}
	got, v1 := storyEpics(t, c, story)
	t.Logf("FINDING after POST: epics=%v version %d → %d", got, v0, v1)
	if fmt.Sprint(got) != fmt.Sprint([]int64{e1}) || v1 != v0 {
		t.Fatalf("after POST: epics=%v version %d → %d", got, v0, v1)
	}

	// 2. The same POST again.
	status, body = linkStatus(c, "POST", rel(e1), map[string]any{"epic": e1, "user_story": story})
	t.Logf("FINDING duplicate POST: %d %s", status, body)
	if status != 400 {
		t.Fatalf("duplicate POST: %d %s", status, body)
	}
	if got, _ := storyEpics(t, c, story); fmt.Sprint(got) != fmt.Sprint([]int64{e1}) {
		t.Fatalf("after duplicate: %v", got)
	}

	// 3. A second epic on the same story.
	if status, body = linkStatus(c, "POST", rel(e2), map[string]any{"epic": e2, "user_story": story}); status != 201 {
		t.Fatalf("second epic: %d %s", status, body)
	}
	got, v2 := storyEpics(t, c, story)
	t.Logf("FINDING two epics: epics=%v version %d", got, v2)
	if len(got) != 2 {
		t.Fatalf("two epics: %v", got)
	}

	// Path and body disagree: which epic wins?
	e3 := epic(project, "E3 "+suffix)
	status, body = linkStatus(c, "POST", rel(e3), map[string]any{"epic": e2, "user_story": story})
	got, _ = storyEpics(t, c, story)
	t.Logf("FINDING POST path epic %d body epic %d: %d %s; epics=%v", e3, e2, status, body, got)

	// 4. DELETE: 204, then 404.
	status, body = linkStatus(c, "DELETE", del(e1), nil)
	t.Logf("FINDING DELETE link: %d %q", status, body)
	if status != 204 {
		t.Fatalf("DELETE: %d %s", status, body)
	}
	got, v3 := storyEpics(t, c, story)
	t.Logf("FINDING after DELETE: epics=%v version %d", got, v3)
	for _, id := range got {
		if id == e1 {
			t.Fatalf("after DELETE the story still has epic %d", e1)
		}
	}
	status, body = linkStatus(c, "DELETE", del(e1), nil)
	t.Logf("FINDING repeated DELETE: %d %s", status, body)
	if status != 404 {
		t.Fatalf("repeated DELETE: %d %s", status, body)
	}
	// GET of one link, for the post-condition alternative.
	status, body = linkStatus(c, "GET", del(e2), nil)
	t.Logf("FINDING GET link: %d %s", status, body)

	// 5. An epic of another project.
	status, body = linkStatus(c, "POST", rel(foreign), map[string]any{"epic": foreign, "user_story": story})
	got, _ = storyEpics(t, c, story)
	t.Logf("FINDING foreign epic: %d %s; epics=%v", status, body, got)
	for _, id := range got {
		if id == foreign {
			t.Logf("FINDING foreign epic was linked")
		}
	}

	// 6. svc: the default member role, then a role without modify_epic.
	t.Logf("FINDING svc modify_epic by default: %v", hasPermission(t, svc, project, "modify_epic"))
	if status, body = linkStatus(svc, "POST", rel(e1), map[string]any{"epic": e1, "user_story": story}); status != 201 {
		t.Logf("FINDING svc POST with the default role: %d %s", status, body)
	}
	var role int64
	for _, m := range probeList(t, c, "memberships", url.Values{"project": {fmt.Sprint(project)}}) {
		if m.int("user") == svcID {
			role = m.int("role")
		}
	}
	r := probeDo(t, c, "GET", fmt.Sprintf("roles/%d", role), nil, nil)
	var perms []string
	if err := json.Unmarshal(r["permissions"], &perms); err != nil {
		t.Fatal(err)
	}
	kept := []string{}
	for _, p := range perms {
		if p != "modify_epic" {
			kept = append(kept, p)
		}
	}
	probeDo(t, c, "PATCH", fmt.Sprintf("roles/%d", role), nil, map[string]any{"permissions": kept})
	if hasPermission(t, svc, project, "modify_epic") {
		t.Fatal("svc still has modify_epic")
	}
	status, body = linkStatus(svc, "POST", rel(e3), map[string]any{"epic": e3, "user_story": story})
	t.Logf("FINDING svc POST without modify_epic: %d %s", status, body)
	status, body = linkStatus(svc, "DELETE", del(e2), nil)
	t.Logf("FINDING svc DELETE without modify_epic: %d %s", status, body)
	got, _ = storyEpics(t, c, story)
	t.Logf("FINDING final epics=%v", got)
}
