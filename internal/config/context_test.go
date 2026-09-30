package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoFileWalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(deep, 0o755)
	_ = os.WriteFile(filepath.Join(root, ".taiga.toml"), []byte("url = \"https://t.example\"\nproject = \"infra-2025\"\n"), 0o644)
	rf, path, err := FindRepoFile(deep)
	if err != nil || path != filepath.Join(root, ".taiga.toml") || rf.Project != "infra-2025" {
		t.Fatalf("%+v %q %v", rf, path, err)
	}
	_, path, err = FindRepoFile(t.TempDir())
	if err != nil || path != "" {
		t.Fatalf("no file: %q %v", path, err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, ".taiga.toml"), []byte("url = \"https://repo.example\"\nproject = \"repo-proj\"\n"), 0o644)
	file := File{DefaultHost: "https://cfg.example", Hosts: []Host{{URL: "https://cfg.example", Project: "cfg-proj"}, {URL: "https://repo.example", Project: "host-proj"}}}
	base := Inputs{Env: envMap(nil), Cwd: repo, File: file, ConfigPath: "/c/config.toml"}

	c, err := Resolve(base)
	if err != nil || c.URL.Value != "https://repo.example" || c.URL.Source != "file:"+filepath.Join(repo, ".taiga.toml") || c.Project.Value != "repo-proj" {
		t.Fatalf("repo level: %+v %v", c, err)
	}

	in := base
	in.Env = envMap(map[string]string{"TAIGA_URL": "https://env.example/", "TAIGA_PROJECT": "env-proj"})
	c, _ = Resolve(in)
	if c.URL.Value != "https://env.example" || c.URL.Source != "env:TAIGA_URL" || c.Project.Source != "env:TAIGA_PROJECT" {
		t.Fatalf("env level: %+v", c)
	}

	in.FlagURL, in.FlagProject = "https://flag.example", "flag-proj"
	c, _ = Resolve(in)
	if c.URL.Source != "flag" || c.Project.Value != "flag-proj" {
		t.Fatalf("flag level: %+v", c)
	}

	in = Inputs{Env: envMap(nil), Cwd: t.TempDir(), File: file, ConfigPath: "/c/config.toml"}
	c, _ = Resolve(in)
	if c.URL.Value != "https://cfg.example" || c.URL.Source != "config:/c/config.toml" || c.Project.Value != "cfg-proj" {
		t.Fatalf("config level: %+v", c)
	}
}

func TestResolveNoURL(t *testing.T) {
	_, err := Resolve(Inputs{Env: envMap(nil), Cwd: t.TempDir()})
	if err == nil {
		t.Fatal("expected config_no_url")
	}
}
