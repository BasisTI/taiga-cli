package cli

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func run(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, strings.NewReader(""), &out, &errOut, func(k string) string { return env[k] }, false)
	return out.String(), errOut.String(), code
}

func TestVersionPrintsVersion(t *testing.T) {
	oldVersion := Version
	t.Cleanup(func() { Version = oldVersion })
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
	if !strings.Contains(errOut, `"code": "usage"`) {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestInvalidOutputIsUsageError(t *testing.T) {
	out, errOut, code := run(t, nil, "--output", "yaml", "version")
	if code != 2 || out != "" {
		t.Fatalf("exit = %d, out = %q, stderr = %s", code, out, errOut)
	}
	if !strings.Contains(errOut, "error [usage]") || !strings.Contains(errOut, `invalid --output "yaml"`) {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestResolveVersion(t *testing.T) {
	info := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/BasisTI/taiga-cli", Version: v}}
	}
	for _, tc := range []struct {
		ldflags string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"0.1.0", info("v0.2.0"), true, "0.1.0"}, // ldflags (GoReleaser) wins
		{"dev", info("v0.1.0"), true, "0.1.0"},   // go install ...@v0.1.0
		{"dev", info("v0.1.1-0.20260930120000-abcdef123456"), true, "0.1.1-0.20260930120000-abcdef123456"},
		{"dev", info("v0.1.0+dirty"), true, "0.1.0+dirty"},
		{"dev", info("(devel)"), true, "dev"}, // go build in a checkout
		{"dev", info(""), true, "dev"},
		{"dev", info("latest"), true, "dev"},
		{"dev", info("v1"), true, "dev"},
		{"dev", nil, false, "dev"}, // no build info
	} {
		if got := resolveVersion(tc.ldflags, tc.info, tc.ok); got != tc.want {
			t.Errorf("resolveVersion(%q, %v): %q, want %q", tc.ldflags, tc.info, got, tc.want)
		}
	}
}
