package config

import (
	"os"
	"path/filepath"
	"testing"
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
