package cli

import (
	"strings"
	"testing"
)

// signedPhoto is a user photo as Taiga sends it: a media URL with a signed token.
const signedPhoto = "http://t/media/user/a/b/photo.png.80x80_q85_crop.png?token=PHOTOSECRET%3A1"

// Story outputs carry user objects (owner_extra_info, assigned_to_extra_info, the history
// user): their photos keep their key and URL, with the token value hidden.
func TestStoryOutputsHideSignedPhotos(t *testing.T) {
	f, _ := commentFake(t)
	user := func() map[string]any {
		return map[string]any{"id": 5, "username": "admin", "photo": signedPhoto, "big_photo": strings.Replace(signedPhoto, "80x80", "300x300", 1)}
	}
	for _, id := range []int64{6808, 6809, 6810} {
		f.stories[id]["owner_extra_info"] = user()
		f.stories[id]["assigned_to_extra_info"] = user()
		f.stories[id]["assigned_users_extra_info"] = []any{user()}
	}
	f.stories[6808]["tags"] = []any{[]any{"token=TAGSECRET", nil}}
	for _, e := range f.history[6808] {
		e["user"] = map[string]any{"pk": 5, "username": "admin", "photo": signedPhoto}
	}
	f.values["userstories/custom-attributes-values/6808"]["attributes_values"] = map[string]any{"27": "see " + signedPhoto}
	for _, args := range [][]string{
		{"story", "get", "246"}, {"story", "get", "246", "--output", "text"},
		{"story", "list"}, {"story", "list", "--output", "text"},
		{"story", "update", "246", "--subject", "new"}, {"story", "close", "248", "--status", "Done"},
		{"story", "comments", "246"}, {"story", "comments", "246", "--output", "text"},
		{"story", "field", "list", "246"}, {"story", "field", "list", "246", "--output", "text"},
	} {
		out, stderr, code := runIn(t, f.env(), "", args...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		if strings.Contains(out+stderr, "SECRET") {
			t.Errorf("%v leaked a token: %s", args, out)
		}
	}
	// The photo keys stay, with the URL and the token key; only the value goes.
	out, _, _ := runIn(t, f.env(), "", "story", "get", "246")
	if !strings.Contains(out, `"photo": "http://t/media/user/a/b/photo.png.80x80_q85_crop.png?token=…"`) || !strings.Contains(out, `"big_photo"`) {
		t.Fatalf("%s", out)
	}
}
