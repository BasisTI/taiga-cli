//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// TestProbeCatalogs records the read contract US #253 relies on: the project list and its
// member filter, the milestone and epic catalogs, their filters, the epic module switch and
// the project fields the diagnosis reads. It uses projects of its own.
func TestProbeCatalogs(t *testing.T) {
	c := probeClient(t)
	svc := svcClient(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	shared := ensureProject(t, c, "cli-test-probe-catalogs-"+suffix)
	alone := ensureProject(t, c, "cli-test-probe-catalogs-alone-"+suffix)
	svcID := userID(t, c, shared, testtaiga.ServiceUser)
	ensureMember(t, c, shared, svcID, testtaiga.ServiceEmail)
	me := func(c *Client) int64 { return probeDo(t, c, "GET", "users/me", nil, nil).int("id") }
	adminID := me(c)
	if me(svc) != svcID {
		t.Fatalf("users/me of svc = %d, users?project= says %d", me(svc), svcID)
	}
	slugs := func(c *Client, q url.Values) map[int64]bool {
		out := map[int64]bool{}
		for _, p := range probeList(t, c, "projects", q) {
			out[p.int("id")] = true
		}
		return out
	}

	// 1. projects?member=<users/me id>: only the projects the account is a member of.
	member := url.Values{"member": {fmt.Sprint(svcID)}}
	if got := slugs(svc, member); !got[shared] || got[alone] {
		t.Fatalf("svc member=%d: shared=%v alone=%v", svcID, got[shared], got[alone])
	}
	if got := slugs(svc, nil); !got[shared] || got[alone] {
		t.Fatalf("svc without member: shared=%v alone=%v (alone is private)", got[shared], got[alone])
	}
	// A public project svc is not a member of: listed without the member filter, not with it.
	public := ensureProject(t, c, "cli-test-probe-catalogs-public-"+suffix)
	probeDo(t, c, "PATCH", fmt.Sprintf("projects/%d", public), nil, map[string]any{"is_private": false})
	if got := slugs(svc, nil); !got[public] {
		t.Fatalf("svc does not list a public project without the member filter")
	}
	if got := slugs(svc, member); got[public] {
		t.Fatalf("member filter lists a public project svc is not a member of")
	}
	// The superuser and a project it is not a member of (created by svc).
	svcOwn := probeDo(t, svc, "POST", "projects", nil, map[string]any{"name": "cli-test-probe-catalogs-svc-" + suffix, "description": "taiga-cli probe", "creation_template": 1})
	adminAll, adminMember := slugs(c, nil), slugs(c, url.Values{"member": {fmt.Sprint(adminID)}})
	t.Logf("FINDING projects: admin id=%d; superuser lists svc's private project: without member=%v, with member=%v",
		adminID, adminAll[svcOwn.int("id")], adminMember[svcOwn.int("id")])
	if adminMember[svcOwn.int("id")] {
		t.Fatalf("member filter lists a project the superuser is not a member of")
	}
	// A private project the account cannot see.
	// A private project the account cannot see: 403 by id, 404 by slug.
	_, err := svc.Do(context.Background(), Request{Method: "GET", Path: fmt.Sprintf("projects/%d", alone)})
	if probeStatus(err) != 403 {
		t.Fatalf("svc GET projects/<private id>: %v", err)
	}
	_, err = svc.Do(context.Background(), Request{Method: "GET", Path: "projects/by_slug", Query: url.Values{"slug": {"cli-test-probe-catalogs-alone-" + suffix}}})
	if probeStatus(err) != 404 {
		t.Fatalf("svc GET projects/by_slug <private>: %v", err)
	}
	// q is a full-text search (to_tsquery on name, tags and description), not a substring.
	if q := slugs(c, url.Values{"q": {"alone"}}); !q[alone] || q[shared] {
		t.Fatalf("q=alone: alone=%v shared=%v", q[alone], q[shared])
	}

	// Project fields the diagnosis reads, as svc (plain member) sees them.
	p := probeDo(t, svc, "GET", fmt.Sprintf("projects/%d", shared), nil, nil)
	for _, k := range []string{"i_am_member", "i_am_admin", "my_permissions", "is_epics_activated", "is_kanban_activated", "is_backlog_activated", "swimlanes", "default_swimlane"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("project has no %q", k)
		}
	}
	var perms []string
	_ = json.Unmarshal(p["my_permissions"], &perms)
	sort.Strings(perms)
	t.Logf("FINDING project: svc i_am_member=%s i_am_admin=%s my_permissions=%v", p["i_am_member"], p["i_am_admin"], perms)

	// 2. x-disable-pagination: honoured by projects, milestones and epics?
	sq := url.Values{"project": {fmt.Sprint(shared)}}
	for i := 0; i < 2; i++ {
		probeDo(t, c, "POST", "milestones", nil, map[string]any{"project": shared, "name": fmt.Sprintf("M%d %s", i, suffix),
			"estimated_start": "2026-10-01", "estimated_finish": "2026-10-15"})
		probeDo(t, c, "POST", "epics", nil, map[string]any{"project": shared, "subject": fmt.Sprintf("E%d %s", i, suffix)})
	}
	for _, tc := range []struct {
		path string
		q    url.Values
	}{{"projects", nil}, {"milestones", sq}, {"epics", sq}} {
		resp, err := c.Do(context.Background(), Request{Method: "GET", Path: tc.path, Query: tc.q, Header: http.Header{"x-disable-pagination": {"True"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("FINDING pagination %s: disabled → x-paginated=%q next=%q", tc.path, resp.Header.Get("x-paginated"), resp.Header.Get("x-pagination-next"))
		if resp.Header.Get("x-pagination-next") != "" {
			t.Fatalf("%s paginates with x-disable-pagination", tc.path)
		}
	}

	// 3. Milestones: closed filter and object size.
	ms := probeList(t, c, "milestones", sq)
	if len(ms) != 2 {
		t.Fatalf("milestones: %d", len(ms))
	}
	probeDo(t, c, "PATCH", fmt.Sprintf("milestones/%d", ms[0].int("id")), nil, map[string]any{"closed": true})
	keys := []string{}
	for k := range ms[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING milestones: keys %v; user_stories=%s", keys, ms[0]["user_stories"])
	for _, v := range []string{"true", "false"} {
		got := probeList(t, c, "milestones", url.Values{"project": {fmt.Sprint(shared)}, "closed": {v}})
		t.Logf("FINDING milestones: closed=%s → %d of 2", v, len(got))
		if len(got) != 1 || string(got[0]["closed"]) != v {
			t.Fatalf("closed=%s not honoured: %d", v, len(got))
		}
	}
	// project__slug and an unknown project filter.
	if got := probeList(t, c, "milestones", url.Values{"project__slug": {"cli-test-probe-catalogs-" + suffix}}); len(got) != 2 {
		t.Fatalf("project__slug: %d", len(got))
	}

	// 4. Epics: list shape, by_ref, module switched off.
	es := probeList(t, c, "epics", sq)
	if len(es) != 2 {
		t.Fatalf("epics: %d", len(es))
	}
	keys = keys[:0]
	for k := range es[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING epics list: keys %v; status_extra_info=%s tags=%s", keys, es[0]["status_extra_info"], es[0]["tags"])
	byRef := probeDo(t, c, "GET", "epics/by_ref", url.Values{"project": {fmt.Sprint(shared)}, "ref": {fmt.Sprint(es[0].int("ref"))}}, nil)
	if byRef.int("id") != es[0].int("id") {
		t.Fatalf("by_ref: %d", byRef.int("id"))
	}
	keys = keys[:0]
	for k := range byRef {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING epics by_ref: keys %v", keys)
	// A ref that is a story, not an epic.
	story := createStory(t, c, shared, "story "+suffix, nil)
	_, err = c.Do(context.Background(), Request{Method: "GET", Path: "epics/by_ref", Query: url.Values{"project": {fmt.Sprint(shared)}, "ref": {fmt.Sprint(story.int("ref"))}}})
	if probeStatus(err) != 404 {
		t.Fatalf("epics/by_ref with a story ref: %v", err)
	}

	// 5. related_userstories.
	epicID := es[0].int("id")
	probeDo(t, c, "POST", fmt.Sprintf("epics/%d/related_userstories", epicID), nil, map[string]any{"epic": epicID, "user_story": story.int("id")})
	rel := probeList(t, c, fmt.Sprintf("epics/%d/related_userstories", epicID), nil)
	if len(rel) != 1 {
		t.Fatalf("related_userstories: %d", len(rel))
	}
	t.Logf("FINDING related_userstories: %v", map[string]string{"user_story": string(rel[0]["user_story"]), "epic": string(rel[0]["epic"]), "order": string(rel[0]["order"])})
	full := probeDo(t, c, "GET", fmt.Sprintf("epics/%d", epicID), nil, nil)
	t.Logf("FINDING epic detail user_stories_counts=%s", full["user_stories_counts"])
	st := probeDo(t, c, "GET", fmt.Sprintf("userstories/%d", story.int("id")), nil, nil)
	t.Logf("FINDING story epics=%s", st["epics"])

	// 6. Epic filters.
	statuses := probeList(t, c, "epic-statuses", sq)
	var closed int64
	for _, s := range statuses {
		if string(s["is_closed"]) == "true" {
			closed = s.int("id")
			break
		}
	}
	if closed == 0 {
		t.Fatal("no closed epic status")
	}
	cur := probeDo(t, c, "GET", fmt.Sprintf("epics/%d", es[1].int("id")), nil, nil)
	probeDo(t, c, "PATCH", fmt.Sprintf("epics/%d", es[1].int("id")), nil, map[string]any{"version": cur.int("version"), "status": closed})
	for _, f := range []url.Values{
		{"status__is_closed": {"true"}}, {"status__is_closed": {"false"}},
		{"q": {"E1"}}, {"q": {"nothing-matches-" + suffix}},
	} {
		qq := url.Values{"project": {fmt.Sprint(shared)}}
		for k, v := range f {
			qq[k] = v
		}
		refs := []string{}
		for _, e := range probeList(t, c, "epics", qq) {
			refs = append(refs, fmt.Sprint(e.int("ref")))
		}
		t.Logf("FINDING epics filter %v → [%s]", f, strings.Join(refs, " "))
		want := map[string]string{"true": "2", "false": "1", "E1": "2", "nothing-matches-" + suffix: ""}[strings.Join(append(f["status__is_closed"], f["q"]...), "")]
		if strings.Join(refs, " ") != want {
			t.Fatalf("epics filter %v: got [%s] want [%s]", f, strings.Join(refs, " "), want)
		}
	}

	// Module switched off: list, by_ref and detail still answer 200, as admin and as svc. The
	// switch only hides the module in the web UI; the API does not check it.
	probeDo(t, c, "PATCH", fmt.Sprintf("projects/%d", shared), nil, map[string]any{"is_epics_activated": false})
	for _, who := range []struct {
		name string
		c    *Client
	}{{"admin", c}, {"svc", svc}} {
		for _, r := range []Request{
			{Method: "GET", Path: "epics", Query: sq},
			{Method: "GET", Path: "epics/by_ref", Query: url.Values{"project": {fmt.Sprint(shared)}, "ref": {fmt.Sprint(es[0].int("ref"))}}},
			{Method: "GET", Path: fmt.Sprintf("epics/%d", epicID)},
		} {
			if _, err := who.c.Do(context.Background(), r); err != nil {
				t.Fatalf("epics off, %s %s: %v", who.name, r.Path, err)
			}
		}
	}
	probeDo(t, c, "PATCH", fmt.Sprintf("projects/%d", shared), nil, map[string]any{"is_epics_activated": true})

	// Users and memberships, for user list.
	mems := probeList(t, c, "memberships", sq)
	keys = keys[:0]
	for k := range mems[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING memberships: keys %v", keys)
	users := probeList(t, c, "users", sq)
	keys = keys[:0]
	for k := range users[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING users?project=: %d users, keys %v", len(users), keys)
}
