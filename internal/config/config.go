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
// Relative XDG_* (and HOME) values are ignored, as the XDG spec requires.
func DefaultPaths(env func(string) string) (Paths, error) {
	abs := func(k string) string {
		if v := env(k); filepath.IsAbs(v) {
			return v
		}
		return ""
	}
	noBase := func(vars string) (Paths, error) {
		return Paths{}, &output.Error{Code: "config_no_home", Source: "env", Cause: "HOME and " + vars + " are unset or not absolute", Recovery: "set TAIGA_CONFIG and TAIGA_STATE_DIR", Exit: output.ExitUsage}
	}
	home := abs("HOME")
	var p Paths
	if v := env("TAIGA_CONFIG"); v != "" {
		p.ConfigFile = v
		p.SecretsDir = filepath.Join(filepath.Dir(v), "secrets")
	} else {
		configHome := abs("XDG_CONFIG_HOME")
		if configHome == "" {
			if home == "" {
				return noBase("XDG_CONFIG_HOME")
			}
			configHome = filepath.Join(home, ".config")
		}
		p.ConfigFile = filepath.Join(configHome, "taiga", "config.toml")
		p.SecretsDir = filepath.Join(configHome, "taiga", "secrets")
	}
	if v := env("TAIGA_STATE_DIR"); v != "" {
		p.StateDir = v
	} else {
		stateHome := abs("XDG_STATE_HOME")
		if stateHome == "" {
			if home == "" {
				return noBase("XDG_STATE_HOME")
			}
			stateHome = filepath.Join(home, ".local", "state")
		}
		p.StateDir = filepath.Join(stateHome, "taiga")
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
	if err := tmp.Sync(); err != nil {
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

// NormalizeURL returns scheme://host[:port] (host lowercased) without trailing slash or /api/v1 suffix.
func NormalizeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	fail := func(cause string) (string, error) {
		return "", &output.Error{Code: "config_invalid_url", Source: "config", Cause: urlForError(u, err) + cause, Recovery: "use https://host (http only for localhost)", Exit: output.ExitUsage}
	}
	if err != nil || u.Host == "" {
		return fail("not an absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(trimmed, "#") {
		return fail("userinfo, query and fragment are not allowed")
	}
	host := strings.ToLower(u.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return fail("scheme must be https")
	}
	p := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v1")
	if strings.Trim(p, "/") != "" {
		return fail("path prefixes are not supported")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// urlForError renders only scheme://host[:port][path] for an error cause: userinfo, query and
// fragment may carry secrets and are never echoed. Input that does not parse with a host is omitted.
func urlForError(u *url.URL, parseErr error) string {
	if parseErr != nil || u.Host == "" {
		return ""
	}
	safe := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	return fmt.Sprintf("%q: ", safe.String())
}
