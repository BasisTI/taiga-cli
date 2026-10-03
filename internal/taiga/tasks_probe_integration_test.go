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

// taskIDs lists the ids of the tasks of project matching extra, sorted.
func taskIDs(t *testing.T, c *Client, project int64, extra url.Values) []int64 {
	t.Helper()
	q := url.Values{"project": {fmt.Sprint(project)}}
	for k, v := range extra {
		q[k] = v
	}
	out := []int64{}
	for _, o := range probeList(t, c, "tasks", q) {
		out = append(out, o.int("id"))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func taskStatus(t *testing.T, c *Client, project int64, closed bool) []int64 {
	t.Helper()
	out := []int64{}
	for _, st := range probeList(t, c, "task-statuses", url.Values{"project": {fmt.Sprint(project)}}) {
		if (string(st["is_closed"]) == "true") == closed {
			out = append(out, st.int("id"))
		}
	}
	return out
}

func taskHistory(t *testing.T, c *Client, id int64, q url.Values) []historyEntry {
	t.Helper()
	raws, err := c.GetAll(context.Background(), fmt.Sprintf("history/task/%d", id), q)
	if err != nil {
		t.Fatal(err)
	}
	out := []historyEntry{}
	for _, raw := range raws {
		var e historyEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// TestProbeTaskContract records the task contract of PR 253-3 (docs/api-notes.md, "Fase 3 — tasks").
func TestProbeTaskContract(t *testing.T) {
	c := probeClient(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	project := ensureProject(t, c, "cli-test-probe-tasks-"+suffix)
	other := ensureProject(t, c, "cli-test-probe-tasks-other-"+suffix)
	admin, svc := userID(t, c, project, testtaiga.AdminUser), userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svc, testtaiga.ServiceEmail)
	open, closed := taskStatus(t, c, project, false), taskStatus(t, c, project, true)

	// 7. task-statuses: is_closed, how many closed ones in the default template.
	t.Logf("FINDING task-statuses: %d open, %d closed (%v)", len(open), len(closed), closed)
	for _, st := range probeList(t, c, "task-statuses", url.Values{"project": {fmt.Sprint(project)}}) {
		t.Logf("FINDING task status %s closed=%s", st["name"], st["is_closed"])
	}
	if len(open) < 2 || len(closed) == 0 {
		t.Fatalf("task statuses: open %v closed %v", open, closed)
	}

	// 1. POST tasks with every field the CLI sends on create.
	s1 := createStory(t, c, project, "task probe story 1 "+suffix, nil)
	s2 := createStory(t, c, project, "task probe story 2 "+suffix, nil)
	due := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	t1 := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "user_story": s1.int("id"), "subject": "Alpha probe " + suffix,
		"status": open[1], "tags": []string{"Probe-Tag"}, "assigned_to": svc, "due_date": due})
	keys := []string{}
	for k := range t1 {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("FINDING POST tasks keys: %v", keys)
	t.Logf("FINDING POST tasks: ref=%d version=%d status=%d tags=%s assigned_to=%s assigned_users=%s due_date=%s due_date_reason=%s is_closed=%s owner=%s milestone=%s created_date=%s",
		t1.int("ref"), t1.int("version"), t1.int("status"), t1["tags"], t1["assigned_to"], t1["assigned_users"], t1["due_date"], t1["due_date_reason"], t1["is_closed"], t1["owner"], t1["milestone"], t1["created_date"])
	t.Logf("FINDING refs: story1=%d story2=%d task=%d (one sequence when consecutive)", s1.int("ref"), s2.int("ref"), t1.int("ref"))
	if t1.int("ref") != s2.int("ref")+1 || t1.int("version") != 1 || t1.int("assigned_to") != svc || string(t1["due_date"]) != fmt.Sprintf("%q", due) {
		t.Fatalf("POST tasks: ref %d after story %d, version %d, assigned_to %s, due_date %s", t1.int("ref"), s2.int("ref"), t1.int("version"), t1["assigned_to"], t1["due_date"])
	}
	if _, ok := t1["assigned_users"]; ok {
		t.Fatalf("a task has assigned_users: %s", t1["assigned_users"])
	}
	if !jsonEqual(t1["tags"], json.RawMessage(`[["probe-tag",null]]`)) {
		t.Fatalf("tags = %s", t1["tags"])
	}
	t2 := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "user_story": s2.int("id"), "subject": "Beta " + suffix, "status": closed[0], "assigned_to": admin})
	t3 := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "subject": "Gamma loose " + suffix})
	t.Logf("FINDING task without user_story: ref=%d user_story=%s", t3.int("ref"), t3["user_story"])

	// Defaults of a minimal POST (subject and story only): what a task create without the
	// optional flags produces, which the recovery of a lost answer compares with.
	proj := probeDo(t, c, "GET", fmt.Sprintf("projects/%d", project), nil, nil)
	minimal := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "user_story": s1.int("id"), "subject": "minimal " + suffix})
	reread := probeDo(t, c, "GET", fmt.Sprintf("tasks/%d", minimal.int("id")), nil, nil)
	t.Logf("FINDING minimal POST: description=%s tags=%s due_date=%s assigned_to=%s status=%d (project default_task_status=%d) is_blocked=%s blocked_note=%s milestone=%s version=%d (re-read %d) attachments=%s is_closed=%s",
		reread["description"], reread["tags"], reread["due_date"], reread["assigned_to"], reread.int("status"), proj.int("default_task_status"),
		reread["is_blocked"], reread["blocked_note"], reread["milestone"], minimal.int("version"), reread.int("version"), reread["attachments"], reread["is_closed"])
	if string(reread["description"]) != `""` || string(reread["tags"]) != "[]" || string(reread["due_date"]) != "null" || string(reread["assigned_to"]) != "null" ||
		proj.int("default_task_status") <= 0 || reread.int("status") != proj.int("default_task_status") || string(reread["is_blocked"]) != "false" ||
		string(reread["blocked_note"]) != `""` || reread.int("version") != 1 || string(reread["attachments"]) != "[]" || string(reread["is_closed"]) != "false" {
		t.Fatalf("minimal POST defaults: %v", reread)
	}
	_, err := c.Do(ctx, Request{Method: "POST", Path: "tasks", Body: map[string]any{"project": other, "user_story": s1.int("id"), "subject": "cross"}})
	t.Logf("FINDING POST task in another project with this project's story: %d %v", probeStatus(err), err)
	if probeStatus(err) != 400 {
		t.Fatalf("cross-project story: %v", err)
	}
	_, err = c.Do(ctx, Request{Method: "POST", Path: "tasks", Body: map[string]any{"project": project, "user_story": s1.int("id"), "subject": "bad date", "due_date": "31/12/2026"}})
	t.Logf("FINDING POST due_date 31/12/2026: %d %v", probeStatus(err), err)
	if probeStatus(err) != 400 {
		t.Fatalf("bad due_date: %v", err)
	}

	// 2. Which list filters the server honours.
	all := taskIDs(t, c, project, nil)
	t.Logf("FINDING all tasks: %v (t1=%d t2=%d t3=%d)", all, t1.int("id"), t2.int("id"), t3.int("id"))
	honoured := map[string]bool{"user_story": true, "status": true, "assigned_to": true, "tags": true, "q": true, "status__is_closed": true}
	for name, q := range map[string]url.Values{
		"user_story":        {"user_story": {fmt.Sprint(s1.int("id"))}},
		"status":            {"status": {fmt.Sprint(open[1])}},
		"assigned_to":       {"assigned_to": {fmt.Sprint(admin)}},
		"tags":              {"tags": {"probe-tag"}},
		"q":                 {"q": {"Alpha"}},
		"status__is_closed": {"status__is_closed": {"true"}},
		"ref":               {"ref": {fmt.Sprint(t2.int("ref"))}},
		"user_story=null":   {"user_story__isnull": {"true"}},
	} {
		got := taskIDs(t, c, project, q)
		t.Logf("FINDING filter %s → %v (honoured=%v)", name, got, len(got) < len(all))
		if (len(got) < len(all)) != honoured[name] {
			t.Fatalf("filter %s: %v of %v", name, got, all)
		}
	}

	// 3. OCC of assigned_to: is it in the history diff of a task?
	path := fmt.Sprintf("tasks/%d", t1.int("id"))
	stale := probeDo(t, c, "GET", path, nil, nil).int("version")
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "assigned_to": admin})
	_, err = c.Do(ctx, Request{Method: "PATCH", Path: path, Body: map[string]any{"version": stale, "assigned_to": svc}})
	t.Logf("FINDING stale PATCH assigned_to after a concurrent assigned_to change: status %d err %v", probeStatus(err), err)
	var ae *APIError
	if !errors.As(err, &ae) || !ae.IsVersionConflict() {
		t.Fatalf("stale assigned_to: want a version conflict, got %v", err)
	}
	_, err = c.Do(ctx, Request{Method: "PATCH", Path: path, Body: map[string]any{"version": stale, "subject": "Alpha probe renamed " + suffix}})
	t.Logf("FINDING stale PATCH subject only after a concurrent assigned_to change: status %d err %v", probeStatus(err), err)
	if err != nil {
		t.Fatalf("stale subject: %v", err)
	}
	inDiff := false
	for _, e := range taskHistory(t, c, t1.int("id"), nil) {
		if _, ok := e.Diff["assigned_to"]; ok {
			t.Logf("FINDING history diff of task carries assigned_to: %v", e.Diff["assigned_to"])
			inDiff = true
			break
		}
	}
	if !inDiff {
		t.Fatal("assigned_to is not in the task history diff")
	}

	// 4. Block.
	str := func(o probeObject, k string) string {
		var v string
		_ = json.Unmarshal(o[k], &v)
		return v
	}
	patch := func(body map[string]any) probeObject {
		cur := probeDo(t, c, "GET", path, nil, nil)
		b := map[string]any{"version": cur.int("version")}
		for k, v := range body {
			b[k] = v
		}
		return probeDo(t, c, "PATCH", path, nil, b)
	}
	r := patch(map[string]any{"blocked_note": "orphan"})
	t.Logf("FINDING note alone: is_blocked=%s note=%q", r["is_blocked"], str(r, "blocked_note"))
	r = patch(map[string]any{"is_blocked": true, "blocked_note": "waiting ção"})
	t.Logf("FINDING block: is_blocked=%s note=%q", r["is_blocked"], str(r, "blocked_note"))
	r = patch(map[string]any{"is_blocked": false})
	t.Logf("FINDING unblock alone: is_blocked=%s note=%q", r["is_blocked"], str(r, "blocked_note"))
	if string(r["is_blocked"]) != "false" || str(r, "blocked_note") != "" {
		t.Fatalf("unblock: %s %q", r["is_blocked"], str(r, "blocked_note"))
	}

	// 5. due_date: null clears; due_date_reason.
	r = patch(map[string]any{"due_date": nil})
	t.Logf("FINDING due_date null: due_date=%s reason=%s", r["due_date"], r["due_date_reason"])
	if string(r["due_date"]) != "null" {
		t.Fatalf("due_date null: %s", r["due_date"])
	}
	r = patch(map[string]any{"due_date": due, "due_date_reason": "probe reason"})
	t.Logf("FINDING due_date with reason: due_date=%s reason=%s", r["due_date"], r["due_date_reason"])
	cur := probeDo(t, c, "GET", path, nil, nil)
	_, err = c.Do(ctx, Request{Method: "PATCH", Path: path, Body: map[string]any{"version": cur.int("version"), "due_date": "2026-02-30"}})
	t.Logf("FINDING due_date 2026-02-30: status %d err %v", probeStatus(err), err)
	if probeStatus(err) != 400 {
		t.Fatalf("due_date 2026-02-30: %v", err)
	}

	// 6. Comment with an old version; history/task format and order.
	stale = probeDo(t, c, "GET", path, nil, nil).int("version")
	patch(map[string]any{"subject": "Alpha probe again " + suffix})
	r, err = probeTry(c, "PATCH", path, map[string]any{"version": stale, "comment": "first comment " + suffix})
	t.Logf("FINDING comment with stale version: version=%d err %v", r.int("version"), err)
	if err != nil {
		t.Fatalf("stale comment: %v", err)
	}
	patch(map[string]any{"comment": "second comment " + suffix})
	comments := taskHistory(t, c, t1.int("id"), url.Values{"type": {"comment"}})
	for _, e := range comments {
		t.Logf("FINDING history/task comment: id=%s type=%d comment=%q user=%s/%d", e.ID, e.Type, e.Comment, e.User.Username, e.User.PK)
	}
	if len(comments) != 2 || comments[0].Comment != "second comment "+suffix || comments[1].Comment != "first comment "+suffix || comments[0].User.PK != admin {
		t.Fatalf("history/task comments (want newest first): %+v", comments)
	}

	// 8. by_ref with this project for a task ref of another project, and for a story ref here.
	foreign := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": other, "subject": "foreign " + suffix})
	for i := 0; i < 10 && foreign.int("ref") <= minimal.int("ref"); i++ {
		foreign = probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": other, "subject": "foreign " + suffix})
	}
	_, err = c.Do(ctx, Request{Method: "GET", Path: "tasks/by_ref", Query: url.Values{"project": {fmt.Sprint(project)}, "ref": {fmt.Sprint(foreign.int("ref"))}}})
	t.Logf("FINDING tasks/by_ref with a ref that only another project has: status %d", probeStatus(err))
	if probeStatus(err) != 404 {
		t.Fatalf("tasks/by_ref with a ref that only another project has: %v", err)
	}
	_, err = c.Do(ctx, Request{Method: "GET", Path: "tasks/by_ref", Query: url.Values{"project": {fmt.Sprint(project)}, "ref": {fmt.Sprint(s1.int("ref"))}}})
	t.Logf("FINDING tasks/by_ref with a story ref of this project: status %d", probeStatus(err))
	if probeStatus(err) != 404 {
		t.Fatalf("tasks/by_ref with a story ref of this project: %v", err)
	}
	_, err = c.Do(ctx, Request{Method: "GET", Path: "userstories/by_ref", Query: url.Values{"project": {fmt.Sprint(project)}, "ref": {fmt.Sprint(t1.int("ref"))}}})
	t.Logf("FINDING userstories/by_ref with a task ref of this project: status %d", probeStatus(err))
	if probeStatus(err) != 404 {
		t.Fatalf("userstories/by_ref with a task ref of this project: %v", err)
	}

	// Sprint: a task created in a story with a milestone, and after the story moves.
	day := time.Now().Format("2006-01-02")
	ms := func(name string) int64 {
		return probeDo(t, c, "POST", "milestones", nil, map[string]any{"project": project, "name": name + " " + suffix, "estimated_start": day, "estimated_finish": day}).int("id")
	}
	m1, m2 := ms("sprint 1"), ms("sprint 2")
	s3 := createStory(t, c, project, "task probe story 3 "+suffix, map[string]any{"milestone": m1})
	t4 := probeDo(t, c, "POST", "tasks", nil, map[string]any{"project": project, "user_story": s3.int("id"), "subject": "in sprint " + suffix})
	t.Logf("FINDING task created in a story of milestone %d: milestone=%s", m1, t4["milestone"])
	patchStory(t, c, s3.int("id"), map[string]any{"milestone": m2})
	moved := probeDo(t, c, "GET", fmt.Sprintf("tasks/%d", t4.int("id")), nil, nil)
	t.Logf("FINDING after the story moves to milestone %d: task milestone=%s", m2, moved["milestone"])
	if t4.int("milestone") != m1 || moved.int("milestone") != m2 {
		t.Fatalf("the task does not follow the story's sprint: %d then %d", t4.int("milestone"), moved.int("milestone"))
	}

	// Closing: is_closed follows the status.
	r = patch(map[string]any{"status": closed[0]})
	t.Logf("FINDING closed status: is_closed=%s finished_date=%s", r["is_closed"], r["finished_date"])
	if string(r["is_closed"]) != "true" {
		t.Fatalf("closed status: is_closed=%s", r["is_closed"])
	}
}

func probeTry(c *Client, method, path string, body any) (probeObject, error) {
	resp, err := c.Do(context.Background(), Request{Method: method, Path: path, Body: body})
	if err != nil {
		return nil, err
	}
	var o probeObject
	return o, json.Unmarshal(resp.Body, &o)
}
