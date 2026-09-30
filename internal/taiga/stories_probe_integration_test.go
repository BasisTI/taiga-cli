//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// These probes pin down the Taiga 6.7 story contract the curated commands depend on
// (docs/api-notes.md, "Fase 2"). They only run against the loopback Taiga of compose.test.yml.

// probeProjectB is a second disposable project, so the same ref exists in two projects.
const probeProjectB = "cli-test-probe-b"

func probeClient(t *testing.T) *Client {
	t.Helper()
	base := testtaiga.URL() // loopback gate before any login or write
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	return New(base, StaticToken{Type: "Bearer", Value: token}, WithRetryWait(0))
}

type probeObject map[string]json.RawMessage

func (o probeObject) int(key string) int64 {
	var n int64
	_ = json.Unmarshal(o[key], &n)
	return n
}

func probeDo(t *testing.T, c *Client, method, path string, q url.Values, body any) probeObject {
	t.Helper()
	resp, err := c.Do(context.Background(), Request{Method: method, Path: path, Query: q, Body: body})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var o probeObject
	if err := json.Unmarshal(resp.Body, &o); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	return o
}

func probeList(t *testing.T, c *Client, path string, q url.Values) []probeObject {
	t.Helper()
	raws, err := c.GetAll(context.Background(), path, q)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	out := make([]probeObject, 0, len(raws))
	for _, raw := range raws {
		var o probeObject
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

func probeStatus(err error) int {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// ensureProject returns the id of project slug, creating it (with epics enabled) when absent.
func ensureProject(t *testing.T, c *Client, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	resp, err := c.Do(ctx, Request{Method: "GET", Path: "projects/by_slug", Query: url.Values{"slug": {slug}}})
	var p probeObject
	switch {
	case err == nil:
		if err := json.Unmarshal(resp.Body, &p); err != nil {
			t.Fatal(err)
		}
	case probeStatus(err) == 404:
		p = probeDo(t, c, "POST", "projects", nil, map[string]any{"name": slug, "description": "taiga-cli probe", "creation_template": 1})
	default:
		t.Fatal(err)
	}
	id := p.int("id")
	probeDo(t, c, "PATCH", fmt.Sprintf("projects/%d", id), nil, map[string]any{"is_epics_activated": true})
	return id
}

func createStory(t *testing.T, c *Client, project int64, subject string, extra map[string]any) probeObject {
	t.Helper()
	body := map[string]any{"project": project, "subject": subject}
	for k, v := range extra {
		body[k] = v
	}
	return probeDo(t, c, "POST", "userstories", nil, body)
}

func statusByName(t *testing.T, c *Client, project int64, name string) int64 {
	t.Helper()
	for _, st := range probeList(t, c, "userstory-statuses", url.Values{"project": {fmt.Sprint(project)}}) {
		if string(st["name"]) == fmt.Sprintf("%q", name) {
			return st.int("id")
		}
	}
	t.Fatalf("status %q not found in project %d", name, project)
	return 0
}

// sharedStoryRef returns a ref that is a story in both projects, creating stories in the
// project whose ref counter is behind until one exists.
func sharedStoryRef(t *testing.T, c *Client, pa, pb int64) int64 {
	t.Helper()
	refs := func(p int64) map[int64]bool {
		out := map[int64]bool{}
		for _, o := range probeList(t, c, "userstories", url.Values{"project": {fmt.Sprint(p)}}) {
			out[o.int("ref")] = true
		}
		return out
	}
	ra, rb := refs(pa), refs(pb)
	for i := 0; i < 100; i++ {
		maxA, maxB := int64(0), int64(0)
		for r := range ra {
			if rb[r] {
				return r
			}
			maxA = max(maxA, r)
		}
		for r := range rb {
			maxB = max(maxB, r)
		}
		if maxA <= maxB {
			ra[createStory(t, c, pa, "phase-2 by-ref twin", nil).int("ref")] = true
		} else {
			rb[createStory(t, c, pb, "phase-2 by-ref twin", nil).int("ref")] = true
		}
	}
	t.Fatal("no shared story ref between the probe projects")
	return 0
}

func TestProbeStoryByRef(t *testing.T) {
	c := probeClient(t)
	ctx := context.Background()
	pa := ensureProject(t, c, testtaiga.ProjectSlug)
	pb := ensureProject(t, c, probeProjectB)
	story := createStory(t, c, pa, "phase-2 by-ref probe", nil)
	if v := story.int("version"); v != 1 {
		t.Fatalf("new story version = %d, want 1", v)
	}
	q := url.Values{"project": {fmt.Sprint(pa)}, "ref": {fmt.Sprint(story.int("ref"))}}
	resolved := probeDo(t, c, "GET", "userstories/by_ref", q, nil)
	if resolved.int("id") != story.int("id") || resolved.int("ref") != story.int("ref") || resolved.int("project") != pa {
		t.Fatalf("resolved id=%d ref=%d project=%d, created id=%d", resolved.int("id"), resolved.int("ref"), resolved.int("project"), story.int("id"))
	}
	if _, ok := resolved["version"]; !ok {
		t.Fatal("by_ref response has no version")
	}

	// The same ref in another project resolves to that project's story, never ours.
	shared := sharedStoryRef(t, c, pa, pb)
	for _, p := range []int64{pa, pb} {
		got := probeDo(t, c, "GET", "userstories/by_ref", url.Values{"project": {fmt.Sprint(p)}, "ref": {fmt.Sprint(shared)}}, nil)
		if got.int("project") != p || got.int("ref") != shared {
			t.Fatalf("by_ref project=%d ref=%d returned project=%d ref=%d", p, shared, got.int("project"), got.int("ref"))
		}
	}

	for name, query := range map[string]url.Values{
		"missing ref":     {"project": {fmt.Sprint(pa)}, "ref": {"2147483647"}},
		"missing project": {"project": {"2147483647"}, "ref": {fmt.Sprint(story.int("ref"))}},
		"non-numeric ref": {"project": {fmt.Sprint(pa)}, "ref": {"abc"}},
	} {
		_, err := c.Do(ctx, Request{Method: "GET", Path: "userstories/by_ref", Query: query})
		if probeStatus(err) != 404 {
			t.Fatalf("%s: want 404, got %v", name, err)
		}
	}
	_, err := c.Do(ctx, Request{Method: "GET", Path: "userstories/by_ref", Query: url.Values{"ref": {"1"}}})
	if probeStatus(err) != 400 {
		t.Fatalf("by_ref without project: want 400, got %v", err)
	}
	// POST does not require a version, but accepts one and stores it: never send it on create.
	if v := createStory(t, c, pa, "phase-2 create with version", map[string]any{"version": 7}).int("version"); v != 7 {
		t.Logf("POST with version stored version=%d", v)
	}
	t.Logf("FINDING by_ref returns the story object with version; missing ref/project → 404; no project → 400")
}

// Story links: which fields a PATCH really writes, and how epics are linked.
func TestProbeStoryLinks(t *testing.T) {
	c := probeClient(t)
	ctx := context.Background()
	pa := ensureProject(t, c, testtaiga.ProjectSlug)
	pb := ensureProject(t, c, probeProjectB)
	suffix := fmt.Sprint(time.Now().UnixNano())
	day := time.Now().Format("2006-01-02")
	msA := probeDo(t, c, "POST", "milestones", nil, map[string]any{"project": pa, "name": "probe " + suffix, "estimated_start": day, "estimated_finish": day})
	msB := probeDo(t, c, "POST", "milestones", nil, map[string]any{"project": pb, "name": "probe " + suffix, "estimated_start": day, "estimated_finish": day})
	laneA := probeDo(t, c, "POST", "swimlanes", nil, map[string]any{"project": pa, "name": "probe " + suffix})
	laneB := probeDo(t, c, "POST", "swimlanes", nil, map[string]any{"project": pb, "name": "probe " + suffix})

	// POST accepts milestone and swimlane; tags come back as [name, color] pairs, lower-cased.
	story := createStory(t, c, pa, "links probe", map[string]any{"milestone": msA.int("id"), "swimlane": laneA.int("id"), "tags": []string{"Probe-Tag"}})
	if story.int("milestone") != msA.int("id") || story.int("swimlane") != laneA.int("id") {
		t.Fatalf("POST links: milestone=%s swimlane=%s", story["milestone"], story["swimlane"])
	}
	if !jsonEqual(story["tags"], json.RawMessage(`[["probe-tag",null]]`)) {
		t.Fatalf("tags = %s", story["tags"])
	}
	path := fmt.Sprintf("userstories/%d", story.int("id"))

	// PATCH writes milestone/swimlane (null clears) with the version.
	v := story.int("version")
	patched := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": v, "milestone": nil, "swimlane": nil})
	if string(patched["milestone"]) != "null" || string(patched["swimlane"]) != "null" || patched.int("version") != v+1 {
		t.Fatalf("PATCH clear: %s %s v=%d", patched["milestone"], patched["swimlane"], patched.int("version"))
	}

	// Catalog entries of another project are refused with 403.
	for field, id := range map[string]int64{"milestone": msB.int("id"), "swimlane": laneB.int("id"), "status": statusByName(t, c, pb, "Ready")} {
		_, err := c.Do(ctx, Request{Method: "PATCH", Path: path, Body: map[string]any{"version": v + 1, field: id}})
		if probeStatus(err) != 403 {
			t.Fatalf("cross-project %s: want 403, got %v", field, err)
		}
	}

	// "epics" in a PATCH is silently ignored, though the version is bumped.
	epic := probeDo(t, c, "POST", "epics", nil, map[string]any{"project": pa, "subject": "probe epic " + suffix})
	ignored := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": v + 1, "epics": []int64{epic.int("id")}})
	if string(ignored["epics"]) != "null" || ignored.int("version") != v+2 {
		t.Fatalf("PATCH epics: epics=%s version=%d", ignored["epics"], ignored.int("version"))
	}

	// The link is a separate resource: POST without version, and it does not bump the story version.
	probeDo(t, c, "POST", fmt.Sprintf("epics/%d/related_userstories", epic.int("id")), nil,
		map[string]any{"epic": epic.int("id"), "user_story": story.int("id")})
	linked := probeDo(t, c, "GET", path, nil, nil)
	var epics []probeObject
	if err := json.Unmarshal(linked["epics"], &epics); err != nil || len(epics) != 1 || epics[0].int("id") != epic.int("id") {
		t.Fatalf("epic link missing: %s", linked["epics"])
	}
	if linked.int("version") != v+2 {
		t.Fatalf("epic link changed story version: %d", linked.int("version"))
	}
	t.Logf("FINDING milestone/swimlane/status are PATCH fields (403 across projects); epics need POST epics/<id>/related_userstories, unversioned")
}

func TestProbeStoryListFilters(t *testing.T) {
	c := probeClient(t)
	pb := ensureProject(t, c, probeProjectB)
	suffix := fmt.Sprint(time.Now().UnixNano())
	tag := "filter-" + suffix
	ready := statusByName(t, c, pb, "Ready")
	done := statusByName(t, c, pb, "Done")
	me := probeDo(t, c, "GET", "users/me", nil, nil).int("id")
	epic := probeDo(t, c, "POST", "epics", nil, map[string]any{"project": pb, "subject": "filter epic " + suffix})
	s1 := createStory(t, c, pb, "filter one "+suffix, map[string]any{"status": ready, "tags": []string{tag}, "assigned_users": []int64{me}})
	s2 := createStory(t, c, pb, "filter two "+suffix, map[string]any{"status": done, "tags": []string{tag}})
	probeDo(t, c, "POST", fmt.Sprintf("epics/%d/related_userstories", epic.int("id")), nil,
		map[string]any{"epic": epic.int("id"), "user_story": s1.int("id")})

	refs := func(extra url.Values) []int64 {
		q := url.Values{"project": {fmt.Sprint(pb)}, "tags": {tag}}
		for k, v := range extra {
			q[k] = v
		}
		out := []int64{}
		for _, o := range probeList(t, c, "userstories", q) {
			out = append(out, o.int("ref"))
		}
		return out
	}
	one, two := fmt.Sprint([]int64{s1.int("ref")}), fmt.Sprint([]int64{s2.int("ref")})
	both := fmt.Sprint([]int64{s1.int("ref"), s2.int("ref")})
	for _, tc := range []struct {
		q    url.Values
		want string
	}{
		{url.Values{}, both},
		{url.Values{"status": {fmt.Sprint(ready)}}, one},
		{url.Values{"assigned_users": {fmt.Sprint(me)}}, one},
		{url.Values{"epic": {fmt.Sprint(epic.int("id"))}}, one},
		{url.Values{"status__is_closed": {"true"}}, two},
		{url.Values{"status__is_closed": {"false"}}, one},
		{url.Values{"q": {"filter two " + suffix}}, two},
		// Ignored parameters: the list is not filtered by them.
		{url.Values{"ref": {fmt.Sprint(s1.int("ref"))}}, both},
		{url.Values{"swimlane": {"2147483647"}}, both},
	} {
		if got := fmt.Sprint(refs(tc.q)); got != tc.want {
			t.Errorf("filter %v: got %s want %s", tc.q, got, tc.want)
		}
	}
	t.Logf("FINDING list honours status, assigned_users, epic, status__is_closed, q, tags; ignores ref and swimlane")
}

// users?project= does not restrict to members: membership must be checked in memberships.
func TestProbeUsersCatalogScope(t *testing.T) {
	c := probeClient(t)
	pa := ensureProject(t, c, testtaiga.ProjectSlug)
	q := url.Values{"project": {fmt.Sprint(pa)}}
	members := map[int64]bool{}
	for _, m := range probeList(t, c, "memberships", q) {
		members[m.int("user")] = true
		if _, ok := m["username"]; ok {
			t.Log("memberships now carry username")
		}
	}
	outsiders := 0
	for _, u := range probeList(t, c, "users", q) {
		if !members[u.int("id")] {
			outsiders++
		}
	}
	if outsiders == 0 {
		t.Skip("every listed user is a member; add a non-member (svc) to observe the scope")
	}
	t.Logf("FINDING users?project= lists %d non-member(s); memberships has user ids but no username", outsiders)
}
