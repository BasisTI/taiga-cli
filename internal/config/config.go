// Package config resolves file locations, the user config file and the Taiga URL/project context.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	toml "github.com/pelletier/go-toml/v2"
)

type Paths struct {
	ConfigFile string
	StateDir   string
	SecretsDir string
}

// DefaultPaths honours TAIGA_CONFIG, TAIGA_STATE_DIR and the XDG base directories.
func DefaultPaths(env func(string) string) (Paths, error) {
	home := env("HOME")
	configHome := env("XDG_CONFIG_HOME")
	if configHome == "" {
		if home == "" {
			return Paths{}, &output.Error{Code: "config_no_home", Source: "env", Cause: "HOME and XDG_CONFIG_HOME are unset", Recovery: "set TAIGA_CONFIG and TAIGA_STATE_DIR", Exit: output.ExitUsage}
		}
		configHome = filepath.Join(home, ".config")
	}
	stateHome := env("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	p := Paths{
		ConfigFile: filepath.Join(configHome, "taiga", "config.toml"),
		StateDir:   filepath.Join(stateHome, "taiga"),
		SecretsDir: filepath.Join(configHome, "taiga", "secrets"),
	}
	if v := env("TAIGA_CONFIG"); v != "" {
		p.ConfigFile = v
		p.SecretsDir = filepath.Join(filepath.Dir(v), "secrets")
	}
	if v := env("TAIGA_STATE_DIR"); v != "" {
		p.StateDir = v
	}
	return p, nil
}

type Host struct {
	URL           string   `toml:"url"`
	Username      string   `toml:"username"`
	SecretSource  string   `toml:"secret_source"`
	SecretCommand []string `toml:"secret_command,omitempty"`
	Project       string   `toml:"project,omitempty"`
}

type File struct {
	DefaultHost string `toml:"default_host,omitempty"`
	Hosts       []Host `toml:"hosts"`
}

// Load reads the config file; a missing file is an empty config.
func Load(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, &output.Error{Code: "config_unreadable", Source: "config", Stage: path, Cause: err.Error(), Recovery: "check the file permissions or set TAIGA_CONFIG", Exit: output.ExitUsage}
	}
	if err := toml.Unmarshal(b, &f); err != nil {
		return f, &output.Error{Code: "config_invalid", Source: "config", Stage: path, Cause: err.Error(), Recovery: "fix the TOML syntax", Exit: output.ExitUsage}
	}
	return f, nil
}

// Save writes the config atomically with mode 0600.
func Save(path string, f File) error {
	b, err := toml.Marshal(f)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

// WriteFileAtomic writes via a temp file + rename, creating parent dirs with 0700 and the file with 0600.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (f File) Host(u string) (Host, bool) {
	for _, h := range f.Hosts {
		if h.URL == u {
			return h, true
		}
	}
	return Host{}, false
}

func (f *File) Upsert(h Host) {
	for i := range f.Hosts {
		if f.Hosts[i].URL == h.URL {
			f.Hosts[i] = h
			return
		}
	}
	f.Hosts = append(f.Hosts, h)
}

// NormalizeURL returns scheme://host[:port] without trailing slash or /api/v1 suffix.
func NormalizeURL(raw string) (string, error) {
	fail := func(cause string) (string, error) {
		return "", &output.Error{Code: "config_invalid_url", Source: "config", Cause: fmt.Sprintf("%q: %s", raw, cause), Recovery: "use https://host (http only for localhost)", Exit: output.ExitUsage}
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fail("not an absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fail("userinfo, query and fragment are not allowed")
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return fail("scheme must be https")
	}
	p := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v1")
	if strings.Trim(p, "/") != "" {
		return fail("path prefixes are not supported")
	}
	return u.Scheme + "://" + u.Host, nil
}
