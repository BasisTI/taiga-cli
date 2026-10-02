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
	got := storyJSON(t, svc, "", "story", "get", ref)
	owner, _ := got["owner_extra_info"].(map[string]any)
	photo, _ := owner["photo"].(string)
	if !strings.Contains(photo, "/media/user/") || !strings.HasSuffix(photo, "?token=…") {
		t.Fatalf("owner photo: %q", photo)
	}
}
