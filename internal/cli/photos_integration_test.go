//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// tinyPNG is a 4x4 red PNG, enough for Taiga to build the avatar thumbnails.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGP4z8AARwzEcQCukw/x0F8jngAAAABJRU5ErkJggg=="

// TestIntegrationStoryOutputsHideUserPhotos gives svc an avatar for the length of the test (the
// seed users have none), so the user objects of a story carry signed photo URLs, as in
// production. The story commands must keep the photo keys and hide the token values.
func TestIntegrationStoryOutputsHideUserPhotos(t *testing.T) {
	env, svc, _ := freshProject(t, "cli-test-photos-"+fmt.Sprint(time.Now().UnixNano()))
	client := taiga.New(testtaiga.URL(), taiga.StaticToken{Type: "Bearer", Value: svc["TAIGA_TOKEN"]})
	img, _ := base64.StdEncoding.DecodeString(tinyPNG)
	resp, err := client.Upload(context.Background(), "users/change_avatar", nil, "avatar", "avatar.png", bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatalf("change_avatar: %v", err)
	}
	t.Cleanup(func() {
		if _, err := client.Do(context.Background(), taiga.Request{Method: "POST", Path: "users/remove_avatar"}); err != nil {
			t.Errorf("remove_avatar: %v", err)
		}
	})
	var me struct {
		Photo    string `json:"photo"`
		BigPhoto string `json:"big_photo"`
	}
	_ = json.Unmarshal(resp.Body, &me)
	tokens := []string{}
	for _, raw := range []string{me.Photo, me.BigPhoto} {
		u, err := url.Parse(raw)
		if err != nil || u.Query().Get("token") == "" {
			t.Fatalf("avatar URL without token: %q", raw)
		}
		tokens = append(tokens, u.Query().Get("token"))
		// The URL is a credential: it opens the file without authentication.
		r, err := http.Get(raw)
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("signed photo without Authorization: %v %v", err, r)
		}
		_ = r.Body.Close()
	}

	story := storyJSON(t, svc, "", "story", "create", "--subject", "photos", "--assignee", testtaiga.ServiceUser)
	ref := fmt.Sprint(story["ref"])
	storyJSON(t, svc, "", "story", "comment", ref, "--body", "with a photo")
	for _, who := range []map[string]string{env, svc} {
		for _, args := range [][]string{
			{"story", "get", ref}, {"story", "get", ref, "--output", "text"},
			{"story", "list"}, {"story", "list", "--output", "text"},
			{"story", "comments", ref}, {"story", "comments", ref, "--output", "text"},
			{"story", "update", ref, "--add-tag", "photo"}, {"story", "field", "list", ref},
		} {
			out, errOut, code := runIn(t, who, "", args...)
			if code != 0 {
				t.Fatalf("%v: %d %s", args, code, errOut)
			}
			for _, tok := range tokens {
				for _, form := range []string{tok, url.QueryEscape(tok)} {
					if strings.Contains(out+errOut, form) {
						t.Fatalf("%v printed the photo token", args)
					}
				}
			}
		}
	}
	// Dry-run plans: the signed photo already in the description, or given on input, is
	// printed without its token; the real write stores it as given.
	if _, errOut, code := runIn(t, svc, "![photo]("+me.Photo+")", "story", "update", ref, "--description-file", "-"); code != 0 {
		t.Fatalf("store the photo in the description: %d %s", code, errOut)
	}
	raw := storyJSON(t, svc, "", "api", "GET", "userstories/by_ref", "--query", "project="+fmt.Sprint(story["project"]), "--query", "ref="+ref)
	if d := fmt.Sprint(raw["description"]); !strings.Contains(d, tokens[0]) && !strings.Contains(d, url.QueryEscape(tokens[0])) {
		t.Fatalf("the real write did not store the original URL (%d bytes)", len(d))
	}
	for _, mode := range []string{"json", "text"} {
		for _, c := range []struct {
			stdin string
			args  []string
		}{
			{"", []string{"story", "update", ref, "--append-description", "ordinary note", "--dry-run"}},
			{"", []string{"story", "comment", ref, "--body", me.BigPhoto, "--dry-run"}},
			{me.Photo, []string{"story", "create", "--subject", "plan", "--description-file", "-", "--dry-run"}},
		} {
			out, errOut, code := runIn(t, svc, c.stdin, append(c.args, "--output", mode)...)
			if code != 0 {
				t.Fatalf("%v %s: %d %s", c.args, mode, code, errOut)
			}
			for _, tok := range tokens {
				if strings.Contains(out+errOut, tok) || strings.Contains(out+errOut, url.QueryEscape(tok)) {
					t.Fatalf("%v %s printed the photo token", c.args, mode)
				}
			}
		}
	}

	// stderr: a date field given the signed photo is refused, and the error hides the token.
	storyJSON(t, env, "", "field", "create", "--kind", "story", "--name", "Entrega", "--type", "date")
	for _, mode := range []string{"json", "text"} {
		out, errOut, code := runIn(t, svc, "", "story", "field", "set", ref, "Entrega="+me.Photo, "--output", mode)
		if code != 2 || out != "" || !strings.Contains(errOut, "token=…") {
			t.Fatalf("date field with the photo (%s): exit %d, stdout %d bytes, stderr %d bytes", mode, code, len(out), len(errOut))
		}
		for _, tok := range tokens {
			if strings.Contains(errOut, tok) || strings.Contains(errOut, url.QueryEscape(tok)) {
				t.Fatalf("stderr (%s) printed the photo token", mode)
			}
		}
	}

	got := storyJSON(t, svc, "", "story", "get", ref)
	owner, _ := got["owner_extra_info"].(map[string]any)
	photo, _ := owner["photo"].(string)
	if !strings.Contains(photo, "/media/user/") || !strings.HasSuffix(photo, "?token=…") {
		t.Fatalf("owner photo: %q", photo)
	}
}
