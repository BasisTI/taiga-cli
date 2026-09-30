package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, strings.NewReader(""), &out, &errOut, func(k string) string { return env[k] })
	return out.String(), errOut.String(), code
}

func TestVersionPrintsVersion(t *testing.T) {
	Version = "1.2.3-test"
	out, _, code := run(t, nil, "version")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(out) != "taiga 1.2.3-test" {
		t.Fatalf("out = %q", out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	_, errOut, code := run(t, nil, "nope")
	if code != 2 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
}
