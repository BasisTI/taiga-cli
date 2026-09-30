//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// probeProjectAssign has admin and svc as members, so a story can have two assignees.
// cli-test keeps svc out: TestProbeUsersCatalogScope needs a non-member there.
const probeProjectAssign = "cli-test-probe-assign"

// ensureMember adds the user with this email to project with its first role, unless
// already a member. It posts the email, not the username: see testtaiga.ServiceEmail.
func ensureMember(t *testing.T, c *Client, project, user int64, email string) {
	t.Helper()
	q := url.Values{"project": {fmt.Sprint(project)}}
	for _, m := range probeList(t, c, "memberships", q) {
		if m.int("user") == user {
			return
		}
	}
	p := probeDo(t, c, "GET", fmt.Sprintf("projects/%d", project), nil, nil)
	var roles []struct{ ID int64 }
	if err := json.Unmarshal(p["roles"], &roles); err != nil || len(roles) == 0 {
		t.Fatalf("roles: %v %s", err, p["roles"])
	}
	probeDo(t, c, "POST", "memberships", nil, map[string]any{"project": project, "role": roles[0].ID, "username": email})
}

func userID(t *testing.T, c *Client, project int64, username string) int64 {
	t.Helper()
	for _, u := range probeList(t, c, "users", url.Values{"project": {fmt.Sprint(project)}}) {
		if string(u["username"]) == fmt.Sprintf("%q", username) {
			return u.int("id")
		}
	}
	t.Fatalf("user %s not listed", username)
	return 0
}

// patchStory sends body with the story's current version and returns the answer.
func patchStory(t *testing.T, c *Client, id int64, body map[string]any) probeObject {
	t.Helper()
	path := fmt.Sprintf("userstories/%d", id)
	cur := probeDo(t, c, "GET", path, nil, nil)
	b := map[string]any{"version": cur.int("version")}
	for k, v := range body {
		b[k] = v
	}
	return probeDo(t, c, "PATCH", path, nil, b)
}

func ids(t *testing.T, raw json.RawMessage) []int64 {
	t.Helper()
	out := []int64{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("assigned_users %s: %v", raw, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sorted(xs ...int64) []int64 {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	return xs
}

// Taiga 6.7 answers assigned_users as the stored list plus assigned_to
// (userstories/serializers.py get_assigned_users). These probes pin the consequences.
func TestProbeStoryAssignees(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectAssign)
	admin, svc := userID(t, c, p, testtaiga.AdminUser), userID(t, c, p, testtaiga.ServiceUser)
	ensureMember(t, c, p, svc, testtaiga.ServiceEmail)

	// assigned_to always shows up in assigned_users, even when the list sent omits it.
	s := createStory(t, c, p, "assignees: owner outside list", map[string]any{"assigned_to": svc, "assigned_users": []int64{admin}})
	if got := ids(t, s["assigned_users"]); !reflect.DeepEqual(got, sorted(admin, svc)) {
		t.Fatalf("create: %v", got)
	}
	// Removing the owner from the list alone: 200, version bumps, the owner is still shown.
	before := s.int("version")
	r := patchStory(t, c, s.int("id"), map[string]any{"assigned_users": []int64{admin}})
	if got := ids(t, r["assigned_users"]); r.int("version") != before+1 || !reflect.DeepEqual(got, sorted(admin, svc)) {
		t.Fatalf("remove owner alone: v%d %v", r.int("version"), got)
	}
	t.Log("FINDING removing assigned_to from assigned_users alone is accepted and silently ignored in the answer")
	// Clearing the owner and the list together removes them.
	r = patchStory(t, c, s.int("id"), map[string]any{"assigned_to": nil, "assigned_users": []int64{admin}})
	if got := ids(t, r["assigned_users"]); string(r["assigned_to"]) != "null" || !reflect.DeepEqual(got, []int64{admin}) {
		t.Fatalf("clear owner + list: %s %v", r["assigned_to"], got)
	}

	// An owner never stored in the list disappears when assigned_to changes alone...
	s = createStory(t, c, p, "assignees: implicit owner", map[string]any{"assigned_users": []int64{svc}})
	r = patchStory(t, c, s.int("id"), map[string]any{"assigned_to": admin})
	if got := ids(t, r["assigned_users"]); !reflect.DeepEqual(got, sorted(admin, svc)) {
		t.Fatalf("set owner: %v", got)
	}
	r = patchStory(t, c, s.int("id"), map[string]any{"assigned_to": svc})
	if got := ids(t, r["assigned_users"]); !reflect.DeepEqual(got, []int64{svc}) {
		t.Fatalf("switch owner alone: %v", got)
	}
	t.Log("FINDING changing assigned_to alone drops a previous owner that was not in the stored list")
	// ...and stays when the same PATCH sends the full list.
	s = createStory(t, c, p, "assignees: explicit list", map[string]any{"assigned_users": []int64{svc}})
	patchStory(t, c, s.int("id"), map[string]any{"assigned_to": admin})
	r = patchStory(t, c, s.int("id"), map[string]any{"assigned_to": svc, "assigned_users": []int64{admin, svc}})
	if got := ids(t, r["assigned_users"]); !reflect.DeepEqual(got, sorted(admin, svc)) {
		t.Fatalf("switch owner with list: %v", got)
	}

	// Without assignees on creation the list is empty and assigned_to stays null.
	if got := ids(t, createStory(t, c, p, "assignees: empty", nil)["assigned_users"]); len(got) != 0 {
		t.Fatalf("default assignees: %v", got)
	}
}

func TestProbeStoryBlock(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectAssign)
	s := createStory(t, c, p, "block probe", nil)
	id := s.int("id")
	str := func(o probeObject, k string) string {
		var v string
		_ = json.Unmarshal(o[k], &v)
		return v
	}
	// A note without is_blocked is dropped.
	if r := patchStory(t, c, id, map[string]any{"blocked_note": "orphan"}); str(r, "blocked_note") != "" || string(r["is_blocked"]) != "false" {
		t.Fatalf("note alone: %s %s", r["is_blocked"], r["blocked_note"])
	}
	note := "waiting for \"B6\"\nline 2 ção"
	if r := patchStory(t, c, id, map[string]any{"is_blocked": true, "blocked_note": note}); str(r, "blocked_note") != note || string(r["is_blocked"]) != "true" {
		t.Fatalf("block: %s %s", r["is_blocked"], r["blocked_note"])
	}
	if r := patchStory(t, c, id, map[string]any{"blocked_note": "other"}); str(r, "blocked_note") != "other" {
		t.Fatalf("note while blocked: %s", r["blocked_note"])
	}
	// Unblocking alone clears the note.
	if r := patchStory(t, c, id, map[string]any{"is_blocked": false}); str(r, "blocked_note") != "" {
		t.Fatalf("unblock: %s", r["blocked_note"])
	}
	// Blocked without a note is accepted by the server (the CLI requires one).
	if r := patchStory(t, c, id, map[string]any{"is_blocked": true}); string(r["is_blocked"]) != "true" || str(r, "blocked_note") != "" {
		t.Fatalf("block without note: %s %s", r["is_blocked"], r["blocked_note"])
	}
	t.Log("FINDING blocked_note is kept only while is_blocked; is_blocked=false clears it; a block without note is accepted")
}

// Taiga's OCC is per field (taiga/projects/occ/mixins.py): a stale version is refused only when
// the PATCH sends a field that history recorded as changed since that version.
func TestProbeOCCIsPerField(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectAssign)
	s := createStory(t, c, p, "occ probe", map[string]any{"is_blocked": true})
	path := fmt.Sprintf("userstories/%d", s.int("id"))
	stale := s.int("version")
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "blocked_note": "theirs"})

	r := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "subject": "mine"})
	if r.int("version") != stale+2 {
		t.Fatalf("disjoint field with stale version: v%d", r.int("version"))
	}
	t.Log("FINDING a stale version is accepted when the PATCH sends only fields nobody changed since")
	_, err := c.Do(context.Background(), Request{Method: "PATCH", Path: path, Body: map[string]any{"version": stale, "is_blocked": false}})
	if err != nil {
		t.Fatalf("is_blocked alone after a note change: %v", err)
	}
	t.Log("FINDING is_blocked:false with a stale version is accepted after a concurrent blocked_note change (and clears it)")
	_, err = c.Do(context.Background(), Request{Method: "PATCH", Path: path, Body: map[string]any{"version": stale, "is_blocked": true, "blocked_note": "mine"}})
	if probeStatus(err) != 400 {
		t.Fatalf("overlapping field with stale version: %v", err)
	}
	t.Log("FINDING sending blocked_note with a stale version after a blocked_note change is a version conflict")
}

// assigned_to is a deprecated history field for stories (history/services.py _deprecated_fields),
// so no history entry records it and the per-field OCC can never see a concurrent owner change.
func TestProbeOCCIgnoresAssignedTo(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectAssign)
	admin, svc := userID(t, c, p, testtaiga.AdminUser), userID(t, c, p, testtaiga.ServiceUser)
	ensureMember(t, c, p, svc, testtaiga.ServiceEmail)
	s := createStory(t, c, p, "occ owner probe", map[string]any{"assigned_users": []int64{admin}, "assigned_to": svc})
	path := fmt.Sprintf("userstories/%d", s.int("id"))
	stale := s.int("version")
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "assigned_to": admin})
	r := probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "assigned_to": svc, "assigned_users": []int64{svc}})
	if r.int("version") != stale+2 {
		t.Fatalf("stale owner write: v%d", r.int("version"))
	}
	t.Log("FINDING a stale PATCH of assigned_to is accepted after a concurrent assigned_to change: OCC cannot protect it")
	// A change of the stored list is recorded, so a stale PATCH of assigned_users conflicts.
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": r.int("version"), "assigned_users": []int64{admin, svc}})
	_, err := c.Do(context.Background(), Request{Method: "PATCH", Path: path, Body: map[string]any{"version": r.int("version"), "assigned_users": []int64{svc}}})
	if probeStatus(err) != 400 {
		t.Fatalf("stale list write: %v", err)
	}
	t.Log("FINDING a stale PATCH of assigned_users conflicts after a concurrent change of the stored list")

	// Exception: with an empty stored list, the history snapshot uses [assigned_to] as the list
	// (userstory_freezer), so an owner change is recorded as assigned_users and does conflict.
	s = createStory(t, c, p, "occ owner probe, empty list", map[string]any{"assigned_to": svc})
	path = fmt.Sprintf("userstories/%d", s.int("id"))
	stale = s.int("version")
	probeDo(t, c, "PATCH", path, nil, map[string]any{"version": stale, "assigned_to": admin})
	_, err = c.Do(context.Background(), Request{Method: "PATCH", Path: path, Body: map[string]any{"version": stale, "assigned_to": svc, "assigned_users": []int64{svc}}})
	if probeStatus(err) != 400 {
		t.Fatalf("stale write after owner change with empty stored list: %v", err)
	}
	t.Log("FINDING with an empty stored list, an owner change is recorded as assigned_users and a stale PATCH of the list conflicts")
}
