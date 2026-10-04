package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// Story↔epic links (docs/api-notes.md, "Fase 3 — vínculo de épico"). A link has no version and
// does not change the story's: the CLI fences it itself, re-reading the story's epics before the
// POST and checking them after every write. Taiga's unique (user_story, epic) answers a repeated
// POST with 400 and a repeated DELETE with 404, so running the same command again converges.

// LinkableEpic resolves an epic of the project for a link: by ref, in a project whose epics
// module is on. Taiga links an epic of any project (probe); resolving here keeps it in this one.
func (s *Service) LinkableEpic(ctx context.Context, ref string) (Object, error) {
	if err := s.epicsEnabled(); err != nil {
		return nil, err
	}
	return s.Epic(ctx, ref)
}

// LinkPlan is what --dry-run prints for a write made of several requests.
type LinkPlan struct {
	DryRun   bool        `json:"dry_run"`
	Requests []WritePlan `json:"requests"`
}

// storyLink is one entry of the story's "epics".
type storyLink struct {
	id    int64
	label string // the ref, prefixed with the project slug for an epic of another project
}

func (s *Service) storyLinks(story Object) []storyLink {
	list, _ := story["epics"].([]any)
	out := []storyLink{}
	for _, x := range list {
		e, _ := x.(map[string]any)
		if ID(e["id"]) <= 0 {
			continue
		}
		label := fmt.Sprint(e["ref"])
		if p, ok := e["project"].(map[string]any); ok && ID(p["id"]) != ID(s.Project["id"]) {
			label = fmt.Sprintf("%v#%v", p["slug"], e["ref"])
		}
		out = append(out, storyLink{ID(e["id"]), label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func linkIDs(links []storyLink) []int64 {
	out := []int64{}
	for _, l := range links {
		out = append(out, l.id)
	}
	return out
}

func linkLabels(links []storyLink) []string {
	out := []string{}
	for _, l := range links {
		out = append(out, l.label)
	}
	return out
}

func hasLink(links []storyLink, id int64) bool {
	for _, l := range links {
		if l.id == id {
			return true
		}
	}
	return false
}

func sameLinks(a, b []storyLink) bool {
	return fmt.Sprint(linkIDs(a)) == fmt.Sprint(linkIDs(b))
}

func linkPath(epic int64) string { return fmt.Sprintf("epics/%d/related_userstories", epic) }

func unlinkPath(epic, story int64) string { return fmt.Sprintf("%s/%d", linkPath(epic), story) }

// LinkEpic makes epic one of the story's epics; with replace, the only one. story is the read the
// change is computed from. The new link is created before any old one is removed, so the story
// never ends without an epic; no request is ever repeated. The caller checks --confirm-delete.
func (s *Service) LinkEpic(ctx context.Context, story, epic Object, replace, dry bool) (any, error) {
	result, _, err := s.linkEpic(ctx, story, epic, replace, dry)
	return result, err
}

// linkEpic is LinkEpic that also returns the story as re-read by the postcondition (nil on a
// no-op or a dry run), for the story commands to print.
func (s *Service) linkEpic(ctx context.Context, story, epic Object, replace, dry bool) (any, Object, error) {
	storyID, epicID := ID(story["id"]), ID(epic["id"])
	if storyID <= 0 || epicID <= 0 {
		return nil, nil, fmt.Errorf("invalid story or epic identity")
	}
	before := s.storyLinks(story)
	old := []storyLink{}
	if replace {
		for _, l := range before {
			if l.id != epicID {
				old = append(old, l)
			}
		}
	}
	add := !hasLink(before, epicID)
	if !add && len(old) == 0 {
		return s.linkResult(story, epic, false, false, nil, before), nil, nil
	}
	if dry {
		return s.linkPlan(story, epic, add, old), nil, nil
	}
	current, err := s.storyEpicsNow(ctx, storyID)
	if err != nil {
		return nil, nil, err
	}
	if !sameLinks(before, current) {
		return nil, nil, &output.Error{Code: "version_conflict", Source: "api", Stage: "GET " + fmt.Sprintf("userstories/%d", storyID),
			Cause:    fmt.Sprintf("the epics of story #%v changed since it was read: [%s], now [%s]; nothing was sent", story["ref"], strings.Join(linkLabels(before), ", "), strings.Join(linkLabels(current), ", ")),
			Recovery: "run the command again: it reads the story again", Exit: output.ExitConflict}
	}
	if add {
		if err := s.postLink(ctx, story, epic); err != nil {
			return nil, nil, err
		}
	}
	removed := []storyLink{}
	if len(old) > 0 {
		if removed, err = s.removeOld(ctx, story, epic, before, old); err != nil {
			return nil, nil, err
		}
	}
	after, err := Read(ctx, s.API, fmt.Sprintf("userstories/%d", storyID), nil)
	if err != nil {
		read := output.AsError(err)
		return nil, nil, &output.Error{Code: "write_applied", Source: read.Source, Stage: fmt.Sprintf("GET userstories/%d", storyID),
			Cause:    fmt.Sprintf("the epic links of story #%v were changed, but the story could not be read to check them: %s", story["ref"], read.Error()),
			Recovery: "check the story with `taiga story get`; running the same command again is safe for the link (it converges), not for other changes made with it",
			Exit:     output.ExitUnexpected}
	}
	final := s.storyLinks(after)
	if !hasLink(final, epicID) || (replace && len(final) != 1) {
		return nil, nil, linksMismatch(story, epic, replace, final, "the story does not have the expected epics after the change")
	}
	return s.linkResult(story, epic, true, add, removed, final), after, nil
}

// storyEpicsNow re-reads the story's epics.
func (s *Service) storyEpicsNow(ctx context.Context, storyID int64) ([]storyLink, error) {
	o, err := Read(ctx, s.API, fmt.Sprintf("userstories/%d", storyID), nil)
	if err != nil {
		return nil, err
	}
	if ID(o["id"]) != storyID {
		return nil, fmt.Errorf("GET returned another story than id %d", storyID)
	}
	return s.storyLinks(o), nil
}

// postLink sends the POST once. Its outcome, when not a clear success or refusal, is decided by
// re-reading the story: a 400 is the duplicate answer only if the link is there.
func (s *Service) postLink(ctx context.Context, story, epic Object) error {
	storyID, epicID := ID(story["id"]), ID(epic["id"])
	path := linkPath(epicID)
	resp, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: path, Body: map[string]any{"epic": epicID, "user_story": storyID}})
	if err == nil {
		if answer, derr := Decode(resp.Body); derr == nil && ID(answer["epic"]) == epicID && ID(answer["user_story"]) == storyID {
			return nil
		}
		err = &taiga.UnreadableBodyError{Method: "POST", Path: path, Status: resp.Status, Err: errors.New("the answer is not the link sent")}
	}
	var ae *taiga.APIError
	duplicate := errors.As(err, &ae) && ae.Status == 400
	// A redirect answers a POST that was sent: the client never follows it, and a proxy may
	// have passed the request on, so the outcome is decided by the re-read, never exit 7.
	redirect := ae != nil && ae.Status >= 300 && ae.Status < 400
	var ue *taiga.UnreadableBodyError
	if !duplicate && !redirect && !errors.As(err, &ue) && (taiga.NotSent(err) || !unknownOutcome(err)) {
		return taiga.ToOutput(err)
	}
	// The POST may have failed because its context is done: the check gets its own deadline.
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	now, rerr := s.storyEpicsNow(check, storyID)
	if rerr == nil && hasLink(now, epicID) {
		return nil
	}
	if duplicate && rerr == nil {
		return taiga.ToOutput(err)
	}
	checked := "the story does not show it, but the request may still be running on the server"
	if rerr != nil {
		checked = "the story could not be read to check: " + output.AsError(rerr).Error()
	}
	return &output.Error{Code: "epic_link_unconfirmed", Source: output.AsError(taiga.ToOutput(err)).Source, Stage: "POST " + path,
		Cause:    fmt.Sprintf("story #%v may have been linked to epic #%v: POST %s failed (%v) and %s; no old link was removed", story["ref"], epic["ref"], path, err, checked),
		Recovery: "linking is idempotent: run the same command again (Taiga refuses a duplicate link)", Exit: output.ExitUnexpected}
}

// removeOld deletes each old link once, after the new one is in. A link that appeared since the
// first read stops it before any DELETE: someone else is changing the story's epics.
func (s *Service) removeOld(ctx context.Context, story, epic Object, before, old []storyLink) ([]storyLink, error) {
	storyID, epicID := ID(story["id"]), ID(epic["id"])
	now, err := s.storyEpicsNow(ctx, storyID)
	if err != nil {
		return nil, replaceIncomplete(story, epic, nil, old, "the story could not be read before removing the old epics: "+output.AsError(err).Error(), fmt.Sprintf("GET userstories/%d", storyID), false)
	}
	if !hasLink(now, epicID) {
		return nil, linksMismatch(story, epic, true, now, fmt.Sprintf("epic #%v is no longer linked; no old link was removed", epic["ref"]))
	}
	for _, l := range now {
		if l.id != epicID && !hasLink(before, l.id) {
			return nil, linksMismatch(story, epic, true, now, fmt.Sprintf("epic %s was linked by someone else meanwhile; no old link was removed", l.label))
		}
	}
	removed := []storyLink{}
	for i, l := range old {
		if !hasLink(now, l.id) {
			removed = append(removed, l) // already gone
			continue
		}
		path := unlinkPath(l.id, storyID)
		_, err := s.API.Do(ctx, taiga.Request{Method: "DELETE", Path: path})
		var ae *taiga.APIError
		gone := errors.As(err, &ae) && ae.Status == 404 // removed by an earlier run or by someone else
		if err != nil && !gone {
			// A 4xx refusal (403 without modify_epic) removed nothing and will refuse a rerun too.
			refused := ae != nil && ae.Status >= 400 && ae.Status < 500
			return nil, replaceIncomplete(story, epic, removed, old[i:], fmt.Sprintf("DELETE %s failed: %s", path, output.AsError(taiga.ToOutput(err)).Error()), "DELETE "+path, refused)
		}
		removed = append(removed, l)
	}
	return removed, nil
}

func replaceIncomplete(story, epic Object, removed, remaining []storyLink, why, stage string, refused bool) error {
	note, recovery := " (the first one may or may not be removed)", rerunRecovery+": it re-reads the story and finishes the replacement"
	if refused {
		note = ""
		recovery = "Taiga refused to remove a link, so running the command again will not finish it: check the modify_epic permission with `taiga auth status --diagnose` (an epic of another project needs it there), or remove the remaining links in the web UI; the new link is saved"
	}
	return &output.Error{Code: "epic_replace_incomplete", Source: "api", Stage: stage,
		Cause: fmt.Sprintf("story #%v: linked to epic #%v; removed [%s]; remaining [%s]%s: %s",
			story["ref"], epic["ref"], strings.Join(linkLabels(removed), ", "), strings.Join(linkLabels(remaining), ", "), note, why),
		Recovery: recovery, Exit: output.ExitUnexpected}
}

func linksMismatch(story, epic Object, replace bool, found []storyLink, why string) error {
	want := fmt.Sprintf("epic #%v among its epics", epic["ref"])
	if replace {
		want = fmt.Sprintf("only epic #%v", epic["ref"])
	}
	state := fmt.Sprintf("the link to epic #%v is saved", epic["ref"])
	if !hasLink(found, ID(epic["id"])) {
		state = fmt.Sprintf("epic #%v is not among them", epic["ref"])
	}
	return &output.Error{Code: "epic_links_postcondition_failed", Source: "api", Stage: fmt.Sprintf("GET userstories/%v", story["id"]),
		Cause:    fmt.Sprintf("story #%v should have %s, found [%s]: %s; %s", story["ref"], want, strings.Join(linkLabels(found), ", "), why, state),
		Recovery: "do not re-run the command blindly: someone else changed the story's epics at the same time; check with `taiga story get` and fix what is needed",
		Exit:     output.ExitConflict}
}

// linkPlan lists the requests a run would send: the POST when the link is missing, then one
// DELETE per old epic.
func (s *Service) linkPlan(story, epic Object, add bool, old []storyLink) LinkPlan {
	plan := LinkPlan{DryRun: true, Requests: []WritePlan{}}
	if add {
		plan.Requests = append(plan.Requests, WritePlan{true, "POST", linkPath(ID(epic["id"])), Object{"epic": epic["id"], "user_story": story["id"]}})
	}
	for _, l := range old {
		plan.Requests = append(plan.Requests, WritePlan{true, "DELETE", unlinkPath(l.id, ID(story["id"])), nil})
	}
	return plan
}

func (s *Service) linkResult(story, epic Object, changed, linked bool, removed, final []storyLink) Object {
	return Object{"story": story["ref"], "epic": epic["ref"], "changed": changed, "linked": linked,
		"removed": linkLabels(removed), "epics": linkLabels(final)}
}

// rerunRecovery starts the recovery of a replacement that running the same command finishes.
const rerunRecovery = "run the same command again"

// afterWrite turns an error of the link that follows a story write into code (exit 1): the
// story write is saved, so neither the code nor the exit may read as "safe to repeat" (exit 7,
// version_conflict, epic_link_unconfirmed). The link's own code stays in the cause. The
// recovery starts with doNot and keeps what the link error asked for: an uncertain or
// unfinished link is finished by `finish` (epic link converges); a concurrent change of the
// story's epics is inspected first; any other error keeps its own recovery.
func afterWrite(err error, code, saved, doNot, finish string, ref any) error {
	e := *output.AsError(err)
	switch {
	case e.Code == "epic_link_unconfirmed" || e.Exit == output.ExitNetwork ||
		e.Code == "epic_replace_incomplete" && strings.HasPrefix(e.Recovery, rerunRecovery):
		e.Recovery = fmt.Sprintf("%s; finish the link with `%s` (running it again converges)", doNot, finish)
	case e.Code == "version_conflict":
		e.Recovery = fmt.Sprintf("%s; the story's epics changed meanwhile: check them with `taiga story get %v` and decide before linking with `%s`", doNot, ref, finish)
	case e.Code == "epic_links_postcondition_failed":
		// Someone else is changing the story's epics: no command is offered before inspection.
		e.Recovery = fmt.Sprintf("%s; for the epic link: %s", doNot, e.Recovery)
	case e.Recovery != "":
		e.Recovery = fmt.Sprintf("%s; for the epic link: %s; once that is fixed, finish with `%s`", doNot, e.Recovery, finish)
	default:
		e.Recovery = fmt.Sprintf("%s; check the story with `taiga story get %v` before linking with `%s`", doNot, ref, finish)
	}
	e.Cause = fmt.Sprintf("%s [%s]; %s", saved, e.Code, e.Cause)
	e.Code, e.Exit = code, output.ExitUnexpected
	return &e
}

// CreateStoryWithEpic creates the story and then links it to epic. The story exists once the
// POST succeeds: a failed link is story_created_link_failed, naming the story, never an
// invitation to run the command again (that would create a second story).
func (s *Service) CreateStoryWithEpic(ctx context.Context, body Object, epic Object, dry bool) (any, error) {
	created, err := s.CreateStory(ctx, body, dry)
	if err != nil {
		return nil, err
	}
	if plan, ok := created.(WritePlan); ok {
		return LinkPlan{DryRun: true, Requests: []WritePlan{plan,
			{true, "POST", linkPath(ID(epic["id"])), Object{"epic": epic["id"], "user_story": nil}}}}, nil
	}
	view := created.(Object)
	_, after, err := s.linkEpic(ctx, view, epic, false, false)
	if err != nil {
		return nil, afterWrite(err, "story_created_link_failed", fmt.Sprintf("the story was created as #%v, but linking it to epic #%v failed", view["ref"], epic["ref"]),
			"do not run the create command again (it would create another story)", fmt.Sprintf("taiga epic link %v %v", epic["ref"], view["ref"]), view["ref"])
	}
	if after == nil {
		// Someone else linked the new story first: the created story is the answer.
		return view, nil
	}
	return s.StoryView(after)
}

// UpdateStoryWithEpic writes the field changes (if any) and then links epic, with replace the
// only one. A failed link after an applied PATCH keeps its code and says the fields are saved.
func (s *Service) UpdateStoryWithEpic(ctx context.Context, ref string, p Patch, epic Object, replace, dry, force bool) (any, error) {
	before, err := s.Story(ctx, ref, 0)
	if err != nil {
		return nil, err
	}
	patch, err := BuildPatch(before, p)
	if err != nil {
		return nil, err
	}
	if dry {
		plan := LinkPlan{DryRun: true, Requests: []WritePlan{}}
		if len(patch) > 0 {
			written, err := s.updateFrom(ctx, storyKind, before, p, true, force)
			if err != nil {
				return nil, err
			}
			plan.Requests = append(plan.Requests, written.(WritePlan))
		}
		link, err := s.LinkEpic(ctx, before, epic, replace, true)
		if err != nil {
			return nil, err
		}
		if lp, ok := link.(LinkPlan); ok {
			plan.Requests = append(plan.Requests, lp.Requests...)
		}
		return plan, nil
	}
	var written any
	if len(patch) > 0 {
		if written, err = s.updateFrom(ctx, storyKind, before, p, false, force); err != nil {
			return nil, err
		}
	}
	_, after, err := s.linkEpic(ctx, before, epic, replace, false)
	if err != nil {
		if len(patch) == 0 {
			return nil, err
		}
		flag := ""
		if replace {
			flag = " --replace --confirm-delete"
		}
		return nil, afterWrite(err, "story_updated_link_failed", fmt.Sprintf("the other changes to story #%v were saved, but the epic link failed", before["ref"]),
			"do not run the update command again (it would repeat the other changes)", fmt.Sprintf("taiga epic link %v %v%s", epic["ref"], before["ref"], flag), before["ref"])
	}
	if after == nil {
		// The link was already there: the story is the PATCH result, or the read before it.
		if view, ok := written.(Object); ok {
			return view, nil
		}
		return s.StoryView(before)
	}
	return s.StoryView(after)
}

// DeleteNotConfirmed refuses a replacement without --confirm-delete, before any request (also
// with --dry-run, so a script learns of the missing flag before the real run).
func DeleteNotConfirmed(flag string) error {
	return &output.Error{Code: "delete_not_confirmed", Cause: flag + " removes the story's other epic links and requires --confirm-delete",
		Recovery: "add --confirm-delete; to add the epic without removing the others, leave out " + flag, Exit: output.ExitUsage}
}
