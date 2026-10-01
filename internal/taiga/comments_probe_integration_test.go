//go:build integration

package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

const probeProjectComments = "cli-test-probe-comments"

type historyEntry struct {
	ID                string
	Type              int
	Comment           string
	Diff              map[string]any
	EditCommentDate   *string `json:"edit_comment_date"`
	DeleteCommentDate *string `json:"delete_comment_date"`
	User              struct {
		PK       int64
		Username string
		IsActive bool `json:"is_active"`
	}
}

func historyOf(t *testing.T, c *Client, id int64) []historyEntry {
	t.Helper()
	raws, err := c.GetAll(context.Background(), fmt.Sprintf("history/userstory/%d", id), nil)
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

func storyVersion(t *testing.T, c *Client, id int64) int64 {
	t.Helper()
	return probeDo(t, c, "GET", fmt.Sprintf("userstories/%d", id), nil, nil).int("version")
}

// TestProbeCommentContract: a comment is not a field of the story, so the per-field OCC
// never refuses an old version for it; it still bumps the version and needs a valid one.
func TestProbeCommentContract(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectComments)
	s := createStory(t, c, p, "comment probe", nil)
	id, path := s.int("id"), fmt.Sprintf("userstories/%d", s.int("id"))
	v1 := s.int("version")

	text := "primeiro \"aspas\" acentuação\nlinha 2 **md**"
	if r := probeDo(t, c, "PATCH", path, nil, map[string]any{"comment": text, "version": v1}); r.int("version") != v1+1 {
		t.Fatalf("comment did not bump the version: %s", r["version"])
	}
	patchStory(t, c, id, map[string]any{"subject": "changed by someone else"})
	// The version read before both writes is still accepted for a comment.
	probeDo(t, c, "PATCH", path, nil, map[string]any{"comment": "old version", "version": v1})
	if status, body := probeErr(t, c, "PATCH", path, map[string]any{"comment": "no version"}); status != 400 || body["version"] == nil {
		t.Fatalf("without version: %d %s", status, body)
	}
	if status, _ := probeErr(t, c, "PATCH", path, map[string]any{"comment": "future", "version": storyVersion(t, c, id) + 5}); status != 400 {
		t.Fatalf("future version: %d", status)
	}
	// A blank comment is stored as an entry (the CLI refuses it).
	probeDo(t, c, "PATCH", path, nil, map[string]any{"comment": "   ", "version": storyVersion(t, c, id)})

	h := historyOf(t, c, id)
	want := []string{"   ", "old version", "", text}
	if len(h) != len(want) {
		t.Fatalf("%d entries: %+v", len(h), h)
	}
	for i, e := range h {
		if e.Comment != want[i] || e.Type != 1 || e.User.Username != "admin" || !e.User.IsActive {
			t.Fatalf("entry %d (newest first): %+v", i, e)
		}
		if (e.Comment == "") != (len(e.Diff) > 0) {
			t.Fatalf("entry %d: a comment with diff, or a diff without comment: %+v", i, e)
		}
	}

	// Editing or deleting a comment does not touch the story's version; the text stays readable.
	before := storyVersion(t, c, id)
	q := url.Values{"id": {h[1].ID}}
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: fmt.Sprintf("history/userstory/%d/edit_comment", id), Query: q, Body: map[string]any{"comment": "edited"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: fmt.Sprintf("history/userstory/%d/delete_comment", id), Query: q, Body: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	e := historyOf(t, c, id)[1]
	if e.Comment != "edited" || e.EditCommentDate == nil || e.DeleteCommentDate == nil || storyVersion(t, c, id) != before {
		t.Fatalf("edited and deleted: %+v", e)
	}
}

// TestProbeCommentHistoryPages: the history pages by 30 unless x-disable-pagination, which it
// honours; type=comment keeps only entries with a comment.
func TestProbeCommentHistoryPages(t *testing.T) {
	c := probeClient(t)
	p := ensureProject(t, c, probeProjectComments)
	s := createStory(t, c, p, "history pages probe", nil)
	path := fmt.Sprintf("userstories/%d", s.int("id"))
	for i := 0; i < 31; i++ {
		probeDo(t, c, "PATCH", path, nil, map[string]any{"comment": fmt.Sprint("n", i), "version": s.int("version")})
	}
	patchStory(t, c, s.int("id"), map[string]any{"subject": "diff only"})
	hp := fmt.Sprintf("history/userstory/%d", s.int("id"))
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: hp})
	if err != nil {
		t.Fatal(err)
	}
	var page []json.RawMessage
	_ = json.Unmarshal(resp.Body, &page)
	if len(page) != 30 || resp.Header.Get("x-pagination-next") == "" {
		t.Fatalf("first page: %d next=%q", len(page), resp.Header.Get("x-pagination-next"))
	}
	resp, err = c.Do(context.Background(), Request{Method: "GET", Path: hp, Header: http.Header{"x-disable-pagination": {"True"}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(resp.Body, &page)
	if len(page) != 32 {
		t.Fatalf("disabled pagination: %d", len(page))
	}
	raws, err := c.GetAll(context.Background(), hp, url.Values{"type": {"comment"}})
	if err != nil || len(raws) != 31 {
		t.Fatalf("type=comment: %d %v", len(raws), err)
	}
}
