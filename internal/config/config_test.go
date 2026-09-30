package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultPathsXDG(t *testing.T) {
	p, err := DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "/s", "HOME": "/h"}))
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != "/c/taiga/config.toml" || p.StateDir != "/s/taiga" || p.SecretsDir != "/c/taiga/secrets" {
		t.Fatalf("%+v", p)
	}
}

func TestDefaultPathsOverridesAndHomeFallback(t *testing.T) {
	p, _ := DefaultPaths(envMap(map[string]string{"HOME": "/h", "TAIGA_CONFIG": "/x/cfg.toml", "TAIGA_STATE_DIR": "/y"}))
	if p.ConfigFile != "/x/cfg.toml" || p.StateDir != "/y" {
		t.Fatalf("%+v", p)
	}
	p, _ = DefaultPaths(envMap(map[string]string{"HOME": "/h"}))
	if p.ConfigFile != "/h/.config/taiga/config.toml" || p.StateDir != "/h/.local/state/taiga" {
		t.Fatalf("%+v", p)
	}
}

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"https://agile.basis.com.br":         "https://agile.basis.com.br",
		"https://agile.basis.com.br/":        "https://agile.basis.com.br",
		"https://agile.basis.com.br/api/v1/": "https://agile.basis.com.br",
		"http://localhost:8000":              "http://localhost:8000",
		"http://127.0.0.1:8000/api/v1":       "http://127.0.0.1:8000",
	}
	for in, want := range ok {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeURL(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"http://agile.basis.com.br", "https://u:p@h", "https://h?x=1", "https://h#f", "agile.basis.com.br", ""} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Fatalf("NormalizeURL(%q) must fail", bad)
		}
	}
}

func TestLoadSaveRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taiga", "config.toml")
	f, err := Load(path)
	if err != nil || len(f.Hosts) != 0 {
		t.Fatalf("missing file must load empty: %v %+v", err, f)
	}
	f.Upsert(Host{URL: "https://a.example", Username: "svc", SecretSource: "keyring", Project: "infra-2025"})
	f.Upsert(Host{URL: "https://a.example", Username: "svc2", SecretSource: "keyring"})
	f.DefaultHost = "https://a.example"
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := back.Host("https://a.example")
	if !ok || h.Username != "svc2" || len(back.Hosts) != 1 || back.DefaultHost != "https://a.example" {
		t.Fatalf("%+v", back)
	}
}

func TestNormalizeURLRejectsEmptyQueryAndFragmentAndLowercasesHost(t *testing.T) {
	for _, bad := range []string{"https://h/?", "https://h?", "https://h/#", "https://h#"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Fatalf("NormalizeURL(%q) must fail", bad)
		}
	}
	got, err := NormalizeURL("https://Agile.Basis.COM.BR")
	if err != nil || got != "https://agile.basis.com.br" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = NormalizeURL("http://LocalHost:8000/")
	if err != nil || got != "http://localhost:8000" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNormalizeURLNeverEchoesSecrets(t *testing.T) {
	for _, in := range []string{"https://user:s3cret@h", "https://user:s3cret@h/%zz", "user:s3cret@h", "http://user:s3cret@agile.example"} {
		_, err := NormalizeURL(in)
		if err == nil {
			t.Fatalf("NormalizeURL(%q) must fail", in)
		}
		var oe *output.Error
		if !errors.As(err, &oe) {
			t.Fatalf("%q: not an output.Error: %v", in, err)
		}
		for _, s := range []string{err.Error(), oe.Code, oe.Source, oe.Stage, oe.Cause, oe.Recovery} {
			if strings.Contains(s, "s3cret") {
				t.Fatalf("%q: secret leaked in %q", in, s)
			}
		}
	}
}

func TestWriteFileAtomicPermissionsAndNoLeftovers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "new")
	path := filepath.Join(dir, "f.toml")
	if err := WriteFileAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	di, err := os.Stat(dir)
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v %v", di, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if len(left) != 0 {
		t.Fatalf("leftover temp files: %v", left)
	}
}

func TestDefaultPathsIgnoresRelativeXDGAndNeedsAbsoluteBase(t *testing.T) {
	// XDG_CONFIG_HOME set, HOME and XDG_STATE_HOME unset: the state dir cannot be derived.
	if _, err := DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "/c"})); !isCode(err, "config_no_home") {
		t.Fatalf("want config_no_home, got %v", err)
	}
	p, err := DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "/c", "TAIGA_STATE_DIR": "/st"}))
	if err != nil || p.ConfigFile != "/c/taiga/config.toml" || p.StateDir != "/st" {
		t.Fatalf("%+v %v", p, err)
	}
	// Relative XDG vars are ignored and fall back to HOME.
	p, err = DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "rel/c", "XDG_STATE_HOME": "rel/s", "HOME": "/h"}))
	if err != nil || p.ConfigFile != "/h/.config/taiga/config.toml" || p.StateDir != "/h/.local/state/taiga" {
		t.Fatalf("%+v %v", p, err)
	}
	// Relative XDG vars and no HOME: error, never a relative path.
	if _, err := DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "rel/c", "XDG_STATE_HOME": "/s"})); !isCode(err, "config_no_home") {
		t.Fatalf("want config_no_home, got %v", err)
	}
	// Relative HOME is not a usable base either.
	if _, err := DefaultPaths(envMap(map[string]string{"HOME": "rel"})); !isCode(err, "config_no_home") {
		t.Fatalf("want config_no_home, got %v", err)
	}
}

func isCode(err error, code string) bool {
	var oe *output.Error
	return errors.As(err, &oe) && oe.Code == code
}
