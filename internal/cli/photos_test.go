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

// photoPlanFake has a signed photo in the description of story 246 and in a custom field
// (Link) of the same story, so a plan that keeps what is there carries it.
func photoPlanFake(t *testing.T) (*storyFake, *[]recorded) {
	f, calls := commentFake(t)
	f.stories[6808]["description"] = "![photo](" + signedPhoto + ")"
	f.defs = map[string][]map[string]any{"userstory-custom-attributes": {
		{"id": 40, "name": "Notas", "type": "text", "project": 37},
		{"id": 41, "name": "Link", "type": "url", "project": 37},
	}}
	f.values[values6808] = map[string]any{"user_story": 6808, "version": 1, "attributes_values": map[string]any{"41": signedPhoto}}
	return f, calls
}

// Dry-run plans are curated output too: the token value is hidden in JSON and in text, for
// what Taiga already holds (description, other fields) and for what was given on input.
func TestStoryPlansHideSignedPhotos(t *testing.T) {
	for _, mode := range []string{"json", "text"} {
		for _, c := range []struct {
			args  []string
			stdin string
		}{
			{[]string{"story", "update", "246", "--append-description", "ordinary note", "--dry-run"}, ""},
			{[]string{"story", "field", "set", "246", "Notas=ordinary", "--dry-run"}, ""},
			{[]string{"story", "comment", "246", "--body", signedPhoto, "--dry-run"}, ""},
			{[]string{"story", "create", "--subject", "new", "--description-file", "-", "--dry-run"}, signedPhoto},
		} {
			f, calls := photoPlanFake(t)
			out, stderr, code := runIn(t, f.env(), c.stdin, append(c.args, "--output", mode)...)
			if code != 0 {
				t.Fatalf("%v %s: %d %s", c.args, mode, code, stderr)
			}
			if strings.Contains(out+stderr, "PHOTOSECRET") || !strings.Contains(out, "token=…") {
				t.Errorf("%v %s: %s", c.args, mode, out)
			}
			if len(writes(calls)) != 0 {
				t.Fatalf("dry-run wrote: %+v", writes(calls))
			}
		}
	}
}

// Only the printout is redacted: the real writes send the values as given and as stored.
func TestStoryWritesSendSignedPhotosUnchanged(t *testing.T) {
	f, calls := photoPlanFake(t)
	for _, args := range [][]string{
		{"story", "update", "246", "--append-description", "ordinary note"},
		{"story", "field", "set", "246", "Notas=ordinary"},
		{"story", "comment", "246", "--body", signedPhoto},
	} {
		if _, stderr, code := runIn(t, f.env(), "", args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	if _, stderr, code := runIn(t, f.env(), signedPhoto, "story", "create", "--subject", "new", "--description-file", "-"); code != 0 {
		t.Fatalf("create: %d %s", code, stderr)
	}
	w := writes(calls)
	if len(w) != 4 {
		t.Fatalf("%+v", w)
	}
	for _, c := range w {
		if b := bodyJSON(t, c.body); !strings.Contains(b, "PHOTOSECRET%3A1") {
			t.Errorf("%s %s sent without the original token: %s", c.method, c.path, b)
		}
	}
}
