package taiga

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"
	"time"
)

const sentinel = "SENTINEL"

// leaks reports whether sentinel survives in s or in any decoding of it (HTML entities and
// percent escapes, repeatedly), in any case: a credential is still a credential when escaped.
// It decodes on its own, up to 64 layers, not with the limit of the code under test.
func leaks(s string) bool {
	for range 64 {
		if strings.Contains(strings.ToUpper(s), sentinel) {
			return true
		}
		next := percentDecode(html.UnescapeString(s))
		if next == s {
			return false
		}
		s = next
	}
	return strings.Contains(strings.ToUpper(s), sentinel)
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

// encodeKey spells "token" in one of several encodings, nested up to 23 times: past
// maxDecodings, the whole text must go.
func encodeKey(kind uint8, depth uint8) string {
	k := "token"
	for i := 0; i < int(depth%24); i++ {
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
		prefix, suffix    string
		key, place, depth uint8
	}{{"", "", 0, 0, 0}, {"see ", " end", 5, 2, 1}, {"a=b&", "&c=d", 9, 3, 3}, {"%", "#x", 255, 4, 2},
		{"&amp;", "", 17, 5, 1}, {"t", "", 66, 1, 2}, {"", "", 0, 0, 10}, {"x ", "", 4, 1, 16}, {"", " y", 8, 3, 23}} {
		f.Add(seed.prefix, seed.suffix, seed.key, seed.place, seed.depth)
	}
	f.Fuzz(func(t *testing.T, prefix, suffix string, key, place, depth uint8) {
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
		in := wrap(place, prefix, encodeKey(key, depth), suffix)
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
		layers, settled := decodings(s)
		if !settled {
			return
		}
		for _, layer := range layers {
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

// deepKey encodes the "t" of "token" as %74 and then the '%' n-1 more times: n decodings
// give "token" back.
func deepKey(n int) string {
	return "%" + strings.Repeat("25", n-1) + "74oken"
}

// A key at any depth is a token key: up to the limit its value is cut out; past the limit,
// where the decoding still changes, the whole text goes (fail closed), token or not.
func TestRedactTokensDeepKeys(t *testing.T) {
	for n := 1; n <= 40; n++ {
		in := "https://h/a?" + deepKey(n) + "=SENTINEL"
		got := RedactTokens(in)
		if strings.Contains(got, sentinel) {
			t.Errorf("depth %d: %q", n, got)
		}
		if n <= maxDecodings && got != "https://h/a?"+deepKey(n)+"=…" {
			t.Errorf("depth %d within the limit: %q", n, got)
		}
	}
	deep := "see %" + strings.Repeat("25", maxDecodings+2) + "41 here" // still changing past the limit, no token
	if got := RedactTokens(deep); got != redactedWhole {
		t.Fatalf("unstable decoding must fail closed: %q", got)
	}
}

// Text that only mentions the word, or carries it encoded as a value, is kept as is: closing
// needs a key ending in "token" followed by '=' and a value, in some decoding.
func TestRedactTokensKeepsDeepPlainText(t *testing.T) {
	for _, in := range []string{
		"Use tokenizer=python; see https://h/a?q=%252525252525252574oken without credentials.",
		"q=" + deepKey(12) + "&x=1",
		"token " + deepKey(5) + " no equals",
	} {
		if got := RedactTokens(in); got != in {
			t.Errorf("plain text changed: %q → %q", in, got)
		}
	}
}

// The scan is linear in the text (times the decoding layers): no '=' rescans the rest.
func TestRedactTokensCostIsLinear(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("a=", 32768),
		strings.Repeat("token=x&", 8192),
		strings.Repeat("%25", 21000) + "=",
		strings.Repeat("&amp;=", 10922),
		strings.Repeat("a", 32768) + "=" + strings.Repeat("b=", 16384),
	} {
		start := time.Now()
		RedactTokens(in)
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Errorf("%d bytes (%q…) took %v", len(in), in[:8], d)
		}
	}
}

func BenchmarkRedactTokens64KiB(b *testing.B) {
	for _, c := range []struct{ name, in string }{
		{"plain-pairs", strings.Repeat("a=", 32768)},
		{"tokens", strings.Repeat("token=x&", 8192)},
		{"prose", strings.Repeat("A description with a link https://h/a?x=1&y=2 and words. ", 1150)},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.SetBytes(int64(len(c.in)))
			for b.Loop() {
				RedactTokens(c.in)
			}
		})
	}
}
