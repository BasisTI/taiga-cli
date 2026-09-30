package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BasisTI/taiga-cli/internal/output"
	toml "github.com/pelletier/go-toml/v2"
)

type RepoFile struct {
	URL     string `toml:"url"`
	Project string `toml:"project"`
}

// FindRepoFile walks up from startDir looking for .taiga.toml.
func FindRepoFile(startDir string) (RepoFile, string, error) {
	dir := startDir
	for {
		path := filepath.Join(dir, ".taiga.toml")
		b, err := os.ReadFile(path)
		if err == nil {
			var rf RepoFile
			if err := toml.Unmarshal(b, &rf); err != nil {
				return rf, path, &output.Error{Code: "config_invalid", Source: "file", Stage: path, Cause: err.Error(), Recovery: "fix the TOML syntax", Exit: output.ExitUsage}
			}
			return rf, path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return RepoFile{}, path, &output.Error{Code: "config_unreadable", Source: "file", Stage: path, Cause: err.Error(), Exit: output.ExitUsage}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return RepoFile{}, "", nil
		}
		dir = parent
	}
}

type Value struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

type Context struct {
	URL     Value `json:"url"`
	Project Value `json:"project"`
}

type Inputs struct {
	FlagURL, FlagProject string
	Env                  func(string) string
	Cwd                  string
	File                 File
	ConfigPath           string
}

// Resolve applies flag > env > .taiga.toml > user config for URL and project.
func Resolve(in Inputs) (Context, error) {
	var c Context
	rf, rpath, err := FindRepoFile(in.Cwd)
	if err != nil {
		return c, err
	}
	pick := func(candidates ...Value) Value {
		for _, v := range candidates {
			if v.Value != "" {
				return v
			}
		}
		return Value{}
	}
	fileSrc := "file:" + rpath
	cfgSrc := "config:" + in.ConfigPath
	c.URL = pick(
		Value{in.FlagURL, "flag"},
		Value{in.Env("TAIGA_URL"), "env:TAIGA_URL"},
		Value{rf.URL, fileSrc},
		Value{in.File.DefaultHost, cfgSrc},
	)
	if c.URL.Value == "" {
		return c, &output.Error{Code: "config_no_url", Source: "config", Cause: "no Taiga URL configured", Recovery: "run `taiga auth login --url https://your.taiga`, set TAIGA_URL or add .taiga.toml", Exit: output.ExitUsage}
	}
	norm, err := NormalizeURL(c.URL.Value)
	if err != nil {
		return c, err
	}
	c.URL.Value = norm
	host, _ := in.File.Host(norm)
	c.Project = pick(
		Value{in.FlagProject, "flag"},
		Value{in.Env("TAIGA_PROJECT"), "env:TAIGA_PROJECT"},
		Value{rf.Project, fileSrc},
		Value{host.Project, cfgSrc},
	)
	return c, nil
}
