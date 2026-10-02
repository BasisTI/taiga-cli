package taiga

import (
	"html"
	"strings"
)

// redactedWhole replaces a text in which a token shows up only once decoded, where its value
// cannot be cut out of the original safely.
const redactedWhole = "[redacted: the text carries a credential]"

// maxDecodings bounds the layers of HTML entities and percent escapes undone.
const maxDecodings = 8

// RedactTokens hides the value of every token parameter in s, keeping the rest of the text:
// read output keeps the URLs users wrote, but never a signed link's credential.
//
// Every '=' is examined on its own, so a token inside the value of another parameter
// (link=https://h/a?token=…, credential=token=…) is found too. The key is the run of key
// characters right before the '=', compared after undoing HTML entities and percent escapes
// (%74oken, t&#111;ken, t&amp;#111;ken), in any case, and anything ending in "token" counts
// (access_token). The value runs to the next delimiter. Then every decoding of the result is
// checked again: if a token value still shows up, the whole text is replaced (fail closed).
func RedactTokens(s string) string {
	out := redactValues(s)
	layers := decodings(out)
	if len(layers) > maxDecodings && strings.Contains(strings.ToLower(layers[len(layers)-1]), "token") {
		return redactedWhole // still changing after the limit: no layer can be trusted
	}
	for _, layer := range layers {
		if hasTokenValue(layer) {
			return redactedWhole
		}
	}
	return out
}

// redactValues replaces the value of each token parameter of s, as written in s.
func redactValues(s string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			continue
		}
		start := i
		for start > 0 && encodedKeyByte(s[start-1]) {
			start--
		}
		end := valueEnd(s, i+1)
		if start == i || end == i+1 || !tokenKey(s[start:i]) {
			continue
		}
		b.WriteString(s[last : i+1])
		b.WriteString("…")
		last, i = end, end-1
	}
	b.WriteString(s[last:])
	return b.String()
}

// hasTokenValue reports whether s, read literally, has a token key with a value other than "…".
func hasTokenValue(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			continue
		}
		start := i
		for start > 0 && keyByte(s[start-1]) {
			start--
		}
		value := s[i+1 : valueEnd(s, i+1)]
		if value != "" && value != "…" && strings.HasSuffix(strings.ToLower(s[start:i]), "token") {
			return true
		}
	}
	return false
}

// tokenKey reports whether key, in any of its decodings, ends with "token" in any case.
func tokenKey(key string) bool {
	for _, k := range decodings(key) {
		if strings.HasSuffix(strings.ToLower(k), "token") {
			return true
		}
	}
	return false
}

func keyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-'
}

// encodedKeyByte also takes the characters of percent escapes and HTML entities.
func encodedKeyByte(c byte) bool { return keyByte(c) || c == '%' || c == '&' || c == '#' || c == ';' }

// valueEnd is the index of the delimiter that ends the value starting at i, or len(s).
func valueEnd(s string, i int) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case '&', '#', ' ', '\t', '\n', '\r', '\f', '\v', '"', '\'', '<', '>':
			return i
		}
	}
	return i
}

// decodings returns s and each successive decoding of it until it stops changing, at most
// maxDecodings+1 layers; one more than that means it was still changing.
func decodings(s string) []string {
	out := []string{s}
	for range maxDecodings + 1 {
		next := percentDecode(html.UnescapeString(s))
		if next == s {
			return out
		}
		out = append(out, next)
		s = next
	}
	return out
}

// percentDecode undoes every valid %XX escape and leaves anything else as is ("100%").
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && hexVal(s[i+1]) >= 0 && hexVal(s[i+2]) >= 0 {
			b.WriteByte(byte(hexVal(s[i+1])<<4 | hexVal(s[i+2])))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
