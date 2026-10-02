//go:build integration

package taiga

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// TestProbeSwimlaneContract records the swimlane contract of Taiga 6.7 that US #252 relies on:
// the catalog shape, what the first swimlane does to the stories, where a story without
// swimlane lands, the list filter and the permissions. It uses a project of its own.
func TestProbeSwimlaneContract(t *testing.T) {
	c := probeClient(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	project := ensureProject(t, c, "cli-test-probe-swimlanes-"+suffix)
	other := ensureProject(t, c, "cli-test-probe-swimlanes-other-"+suffix)
	q := url.Values{"project": {fmt.Sprint(project)}}
	projectPath := fmt.Sprintf("projects/%d", project)
	// A project without swimlanes answers null for both, not an empty list.
	if p := probeDo(t, c, "GET", projectPath, nil, nil); string(p["default_swimlane"]) != "null" || string(p["swimlanes"]) != "null" {
		t.Fatalf("new project: default_swimlane=%s swimlanes=%s", p["default_swimlane"], p["swimlanes"])
	}
	if lanes := probeList(t, c, "swimlanes", q); len(lanes) != 0 {
		t.Fatalf("new project lists swimlanes: %v", lanes)
	}

	// 1. The first swimlane takes every story of the project and becomes the default.
	s1 := createStory(t, c, project, "lane one "+suffix, nil)
	s2 := createStory(t, c, project, "lane two "+suffix, nil)
	if string(s1["swimlane"]) != "null" {
		t.Fatalf("story before any swimlane: %s", s1["swimlane"])
	}
	a := probeDo(t, c, "POST", "swimlanes", nil, map[string]any{"project": project, "name": "A " + suffix})
	for _, k := range []string{"id", "name", "order", "project", "statuses"} {
		if _, ok := a[k]; !ok {
			t.Fatalf("swimlane has no %q: %v", k, a)
		}
	}
	if _, ok := a["version"]; ok {
		t.Fatalf("swimlane has a version: %v", a)
	}
	for _, s := range []probeObject{s1, s2} {
		got := probeDo(t, c, "GET", fmt.Sprintf("userstories/%d", s.int("id")), nil, nil)
		if got.int("swimlane") != a.int("id") {
			t.Fatalf("first swimlane did not take story %d: %s", s.int("id"), got["swimlane"])
		}
		// The move is a bulk update: the story version does not change.
		if got.int("version") != s.int("version") {
			t.Fatalf("the move to the first swimlane changed the story version: %d → %d", s.int("version"), got.int("version"))
		}
	}
	if p := probeDo(t, c, "GET", projectPath, nil, nil); p.int("default_swimlane") != a.int("id") {
		t.Fatalf("default_swimlane = %s, want %d", p["default_swimlane"], a.int("id"))
	}

	// 2. A new swimlane goes to the end: order grows, and the list follows it.
	b := probeDo(t, c, "POST", "swimlanes", nil, map[string]any{"project": project, "name": "B " + suffix})
	if b.int("order") <= a.int("order") {
		t.Fatalf("order: A=%d B=%d", a.int("order"), b.int("order"))
	}
	lanes := probeList(t, c, "swimlanes", q)
	if len(lanes) != 2 || lanes[0].int("id") != a.int("id") || lanes[1].int("id") != b.int("id") {
		t.Fatalf("list order: %v", lanes)
	}
	if p := probeDo(t, c, "GET", projectPath, nil, nil); p.int("default_swimlane") != a.int("id") {
		t.Fatalf("second swimlane changed the default: %s", p["default_swimlane"])
	}
	// Same name in the same project: refused (unique_together project, name).
	if status, body := probeErr(t, c, "POST", "swimlanes", map[string]any{"project": project, "name": "A " + suffix}); status != 400 {
		t.Fatalf("duplicate name: %d %v", status, body)
	}

	// 3. A story created without swimlane, once swimlanes exist: where does it land?
	s3 := createStory(t, c, project, "lane three "+suffix, nil)
	// It stays without swimlane: only the first swimlane moves stories, the default is not applied.
	if string(s3["swimlane"]) != "null" {
		t.Fatalf("story created without swimlane: swimlane=%s (default is %d)", s3["swimlane"], a.int("id"))
	}

	// 4. PATCH swimlane with the version, then null: both are applied and bump the version.
	path := fmt.Sprintf("userstories/%d", s1.int("id"))
	cur := probeDo(t, c, "GET", path, nil, nil)
	v := cur.int("version")
	moved := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": v, "swimlane": b.int("id")})
	if moved.int("swimlane") != b.int("id") || moved.int("version") != v+1 {
		t.Fatalf("PATCH swimlane: %s v=%d", moved["swimlane"], moved.int("version"))
	}
	cleared := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": v + 1, "swimlane": nil})
	if string(cleared["swimlane"]) != "null" || cleared.int("version") != v+2 {
		t.Fatalf("PATCH swimlane null: %s v=%d", cleared["swimlane"], cleared.int("version"))
	}
	if reread := probeDo(t, c, "GET", path, nil, nil); string(reread["swimlane"]) != "null" {
		t.Fatalf("re-read after null: %s", reread["swimlane"])
	}

	// 5. List filters: s1 has no swimlane, s2 is in A; put s3 in B.
	probeDo(t, c, "PATCH", fmt.Sprintf("userstories/%d", s3.int("id")), nil, map[string]any{"version": s3.int("version"), "swimlane": b.int("id")})
	refs := func(extra url.Values) string {
		qq := url.Values{"project": {fmt.Sprint(project)}}
		for k, v := range extra {
			qq[k] = v
		}
		out := []string{}
		for _, o := range probeList(t, c, "userstories", qq) {
			out = append(out, fmt.Sprint(o.int("ref")))
		}
		return strings.Join(out, ",")
	}
	all := refs(nil)
	r1, r3 := fmt.Sprint(s1.int("ref")), fmt.Sprint(s3.int("ref"))
	// "swimlane" is ignored; Taiga's filter reads the misspelled "swimnlane" (filters.py), which
	// takes an id or "null". The CLI may send it to shorten the list, but filters locally anyway.
	for _, tc := range []struct {
		q    url.Values
		want string
	}{
		{url.Values{"swimlane": {fmt.Sprint(b.int("id"))}}, all},
		{url.Values{"swimlane": {"null"}}, all},
		{url.Values{"swimnlane": {fmt.Sprint(b.int("id"))}}, r3},
		{url.Values{"swimnlane": {"null"}}, r1},
	} {
		if got := refs(tc.q); got != tc.want {
			t.Errorf("filter %v: got [%s] want [%s]", tc.q, got, tc.want)
		}
	}

	// 6. A plain member reads the catalog and cannot create swimlanes.
	svcID := userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svcID, testtaiga.ServiceEmail)
	svc := svcClient(t)
	if len(probeList(t, svc, "swimlanes", q)) != 2 {
		t.Fatal("svc cannot read the swimlanes")
	}
	if status, _ := probeErr(t, svc, "POST", "swimlanes", map[string]any{"project": project, "name": "svc " + suffix}); status != 403 {
		t.Fatalf("svc POST swimlanes: %d", status)
	}
	// A plain member can still move a story to a swimlane: it is a story field.
	cur = probeDo(t, c, "GET", path, nil, nil)
	if o := probeDo(t, svc, "PATCH", path, nil, map[string]any{"version": cur.int("version"), "swimlane": a.int("id")}); o.int("swimlane") != a.int("id") {
		t.Fatalf("svc PATCH swimlane: %s", o["swimlane"])
	}

	// 7. A swimlane of another project is refused.
	foreign := probeDo(t, c, "POST", "swimlanes", nil, map[string]any{"project": other, "name": "F " + suffix})
	cur = probeDo(t, c, "GET", path, nil, nil)
	if status, body := probeErr(t, c, "PATCH", path, map[string]any{"version": cur.int("version"), "swimlane": foreign.int("id")}); status != 403 {
		t.Fatalf("foreign swimlane: %d %v", status, body)
	}
	if status, body := probeErr(t, c, "PATCH", path, map[string]any{"version": cur.int("version"), "swimlane": 2147483647}); status != 400 || body["swimlane"] == nil {
		t.Fatalf("missing swimlane id: %d %v", status, body)
	}

	// 8. OCC per field: an old version with a disjoint change is accepted; with swimlane changed
	// since that version, a swimlane write is refused.
	cur = probeDo(t, c, "GET", path, nil, nil)
	old := cur.int("version")
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": old, "subject": "lane one renamed " + suffix})
	if o := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": old, "swimlane": b.int("id")}); o.int("swimlane") != b.int("id") {
		t.Fatalf("disjoint old-version PATCH: %s", o["swimlane"])
	}
	if status, body := probeErr(t, c, "PATCH", path, map[string]any{"version": old, "swimlane": nil}); status != 400 || body["version"] == nil {
		t.Fatalf("overlapping old-version PATCH: %d %v", status, body)
	}
	t.Logf("FINDING swimlanes: no version; first one takes every story (version kept) and becomes default; later stories start without swimlane; list filter is \"swimnlane\"; PATCH swimlane/null with version (OCC per field); foreign → 403; svc reads, POST → 403")
}
