package taiga

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"
)

const sentinel = "SENTINEL"

// leaks reports whether sentinel survives in s or in any decoding of it (HTML entities and
// percent escapes, repeatedly), in any case: a credential is still a credential when escaped.
func leaks(s string) bool {
	for _, layer := range decodings(s) {
		if strings.Contains(strings.ToUpper(layer), sentinel) {
			return true
		}
	}
	return false
}

// Every '=' is examined, also inside the value of another key=value, and the key is compared
// decoded (entities and percent escapes, several layers deep).
func TestRedactTokensNeverLeaks(t *testing.T) {
	for _, in := range []string{
		"token=SENTINEL", "TOKEN=SENTINEL", "access_token=SENTINEL", "csrfmiddlewaretoken=SENTINEL", "notoken=SENTINEL",
		"%74oken=SENTINEL", "t%6fken=SENTINEL", "%2574oken=SENTINEL", "%252574oken=SENTINEL", "%25252574oken=SENTINEL",
		"t&#111;ken=SENTINEL", "t&#x6f;ken=SENTINEL", "t&#111ken=SENTINEL", "t&amp;#111;ken=SENTINEL", "t&amp;amp;#111;ken=SENTINEL",
		"t%26%23111%3bken=SENTINEL", "t&#37;6fken=SENTINEL",
		"http://h/a?token=SENTINEL&keep=yes", "http://h/a?%74oken=SENTINEL&keep=yes", "http://h/a?x=1&amp;token=SENTINEL&amp;keep=yes",
		"link=https://h/a?token=SENTINEL", "description=token=SENTINEL", "credential=token=SENTINEL",
		"http://h/a?next=https://h/b?token=SENTINEL", "a=b=c=token=SENTINEL", "x=%74oken%3DSENTINEL",
		"next=https%3A%2F%2Fh%2Fb%3Ftoken%3DSENTINEL", "next=https%253A%252F%252Fh%252Fb%253Ftoken%253DSENTINEL",
		"<a href=\"https://h/a?token=SENTINEL\">x</a>",
	} {
		if got := RedactTokens(in); leaks(got) {
			t.Errorf("RedactTokens(%q) = %q", in, got)
		}
		if got := RedactSecrets(in); leaks(got) {
			t.Errorf("RedactSecrets(%q) = %q", in, got)
		}
	}
}

// Where the token is found in the original text, only its value goes; the rest stays.
func TestRedactTokensKeepsTheText(t *testing.T) {
	for in, want := range map[string]string{
		"http://h/media/a?%74oken=S":            "http://h/media/a?%74oken=…",
		"http://h/a?x=1&%54%4F%4B%45%4E=S&b=2":  "http://h/a?x=1&%54%4F%4B%45%4E=…&b=2",
		"link=https://h/a?token=S&keep=yes":     "link=https://h/a?token=…&keep=yes",
		"description=token=S":                   "description=token=…",
		"t&amp;#111;ken=S and more":             "t&amp;#111;ken=… and more",
		"http://h/a?x=1&amp;token=S&amp;keep=1": "http://h/a?x=1&amp;token=…&amp;keep=1",
	} {
		if got := RedactTokens(in); got != want {
			t.Errorf("RedactTokens(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactTokensLeavesPlainText(t *testing.T) {
	for _, in := range []string{
		"A token is a lexical unit", "Tokens: a=1 b=2", "token_count=7 tokens=keep tokenizer=lexer",
		"nonsecret=abc", "note=The token is not here", "regular Ágil = café", "http://h/a?q=token&next=2",
		"bad %zz=keep", "100% done, a=b", "description=hello=world", "http://h/a?chapter=one&counter=2#token", "token=",
	} {
		if got := RedactTokens(in); got != in {
			t.Errorf("plain text changed: %q → %q", in, got)
		}
	}
}

// When a decoding shows a token the original text does not let us cut out, the whole string goes.
func TestRedactTokensFailsClosed(t *testing.T) {
	in := "x=%74ok%65n%3DSENTINEL"
	if got := RedactTokens(in); leaks(got) || got != redactedWhole {
		t.Fatalf("%q", got)
	}
}

// encodeKey spells "token" in one of several encodings, possibly nested.
func encodeKey(kind uint8) string {
	k := "token"
	for i := 0; i < int(kind%4); i++ {
		switch (kind / 4) % 4 {
		case 0:
			k = strings.ReplaceAll(url.QueryEscape(k), "t", "%74")
		case 1:
			k = strings.Replace(k, "o", "&#111;", 1)
		case 2:
			k = html.EscapeString(k)
		case 3:
			k = url.PathEscape(strings.Replace(k, "k", "&#107;", 1))
		}
	}
	if kind&64 != 0 {
		k = strings.ToUpper(k[:1]) + k[1:]
	}
	if kind&128 != 0 {
		k = "access_" + k
	}
	return k
}

// wrap puts key=SENTINEL inside text: a URL, the value of another key, an HTML attribute.
func wrap(kind uint8, prefix, key, suffix string) string {
	pair := key + "=" + sentinel
	switch kind % 6 {
	case 0:
		return prefix + pair + suffix
	case 1:
		return prefix + "https://h/a?x=1&" + pair + "&y=2" + suffix
	case 2:
		return prefix + "link=https://h/a?" + pair + suffix
	case 3:
		return prefix + "outer=" + pair + suffix
	case 4:
		return prefix + "next=" + url.QueryEscape("https://h/b?"+pair) + suffix
	default:
		return prefix + `<a href="https://h/a?` + html.EscapeString(pair) + `">` + suffix
	}
}

func FuzzRedactTokensHidesSentinel(f *testing.F) {
	for _, seed := range []struct {
		prefix, suffix string
		key, place     uint8
	}{{"", "", 0, 0}, {"see ", " end", 5, 2}, {"a=b&", "&c=d", 9, 3}, {"%", "#x", 255, 4}, {"&amp;", "", 17, 5}, {"t", "", 66, 1}} {
		f.Add(seed.prefix, seed.suffix, seed.key, seed.place)
	}
	f.Fuzz(func(t *testing.T, prefix, suffix string, key, place uint8) {
		if strings.Contains(strings.ToUpper(prefix+suffix), sentinel) {
			return
		}
		if prefix != "" && encodedKeyByte(prefix[len(prefix)-1]) {
			// Glued to the key, the prefix changes it: "&G"+"T&#111;ken" decodes to ">oken", no token.
			prefix += " "
		}
		if len(suffix) > 0 && !strings.ContainsRune("&# \"'<>", rune(suffix[0])) {
			suffix = " " + suffix // the value ends at a delimiter, or the sentinel would not be the whole value
		}
		in := wrap(place, prefix, encodeKey(key), suffix)
		if got := RedactTokens(in); leaks(got) {
			t.Fatalf("RedactTokens(%q) = %q", in, got)
		}
	})
}

func FuzzRedactTokensLeavesPlainText(f *testing.F) {
	for _, seed := range []string{"a=b", "http://h/a?q=1&r=2#f", "100% sure", "x=&amp;y", "tok=en", "Ágil=café"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, layer := range decodings(s) {
			if strings.Contains(strings.ToLower(layer), "token") {
				return
			}
		}
		if got := RedactTokens(s); got != s {
			t.Fatalf("plain text changed: %q → %q", s, got)
		}
	})
}

func ExampleRedactTokens() {
	fmt.Println(RedactTokens("see https://h/a?x=1&token=abc&y=2 and credential=t%6Fken=abc"))
	// Output: see https://h/a?x=1&token=…&y=2 and credential=t%6Fken=…
}
