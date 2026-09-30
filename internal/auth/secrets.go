package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
)

type SecretSource interface {
	Name() string
	Password(ctx context.Context) ([]byte, error)
}

func authErr(code, source, cause, recovery string) *output.Error {
	return &output.Error{Code: code, Source: source, Cause: cause, Recovery: recovery, Exit: output.ExitAuth}
}

type EnvPassword struct{ Env func(string) string }

func (EnvPassword) Name() string { return "env" }
func (e EnvPassword) Configured() bool {
	return e.Env("TAIGA_PASSWORD") != "" || e.Env("TAIGA_PASSWORD_FILE") != ""
}
func (e EnvPassword) Password(context.Context) ([]byte, error) {
	if v := e.Env("TAIGA_PASSWORD"); v != "" {
		return []byte(v), nil
	}
	path := e.Env("TAIGA_PASSWORD_FILE")
	if path == "" {
		return nil, authErr("auth_no_source", "env", "TAIGA_PASSWORD and TAIGA_PASSWORD_FILE are unset", "")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, authErr("password_file_unreadable", "env", fmt.Sprintf("TAIGA_PASSWORD_FILE %s: %v", path, err), "check the path and permissions")
	}
	b = bytes.TrimRight(b, "\r\n")
	if len(b) == 0 {
		return nil, authErr("password_file_empty", "env", "TAIGA_PASSWORD_FILE is empty", "")
	}
	return b, nil
}

var commandTimeout = 10 * time.Second

type CommandSecret struct{ Args []string }

func (CommandSecret) Name() string { return "secret_command" }

type capped struct {
	bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room > 0 {
		if len(p) > room {
			c.Buffer.Write(p[:room])
		} else {
			c.Buffer.Write(p)
		}
	}
	return len(p), nil
}

// stderrPatterns are the only stderr facts that reach the user. The command's stderr and
// argv are never copied: a helper may print the secret there or take it as an argument.
var stderrPatterns = []struct {
	re    *regexp.Regexp
	label string
}{
	{regexp.MustCompile(`(?i)gpg:`), "gpg"},
	{regexp.MustCompile(`(?i)pinentry`), "pinentry"},
	{regexp.MustCompile(`(?i)inappropriate ioctl`), "inappropriate ioctl for device"},
	{regexp.MustCompile(`(?i)no tty`), "no tty"},
}

// classifyStderr names the known patterns in stderr, and whether any gpg/pinentry one matched.
func classifyStderr(b []byte) (string, bool) {
	var labels []string
	for _, p := range stderrPatterns {
		if p.re.Match(b) {
			labels = append(labels, p.label)
		}
	}
	switch {
	case len(labels) > 0:
		return "stderr mentions " + strings.Join(labels, ", ") + " (other stderr text withheld)", true
	case len(bytes.TrimSpace(b)) > 0:
		return "stderr withheld (it may contain the secret)", false
	default:
		return "no stderr output", false
	}
}

func (c CommandSecret) Password(ctx context.Context) ([]byte, error) {
	if len(c.Args) == 0 {
		return nil, authErr("secret_command_not_found", "secret_command", "secret_command is empty", "set secret_command in the config")
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	stdout := &capped{max: 64 << 10}
	stderr := &capped{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	detail, gpg := classifyStderr(stderr.Bytes())
	recovery := "run the secret command by hand to see what it needs"
	if gpg {
		recovery = "run the secret command once with `export GPG_TTY=$(tty)` in an interactive terminal outside the sandbox to unlock gpg-agent, then retry"
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, authErr("secret_command_timeout", "secret_command", fmt.Sprintf("no answer after %s; %s", commandTimeout, detail), recovery)
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		return nil, authErr("secret_command_not_found", "secret_command", "executable not found: "+c.Args[0], "fix secret_command in the config (absolute path, no shell)")
	case err != nil:
		var ee *exec.ExitError
		code := -1
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return nil, authErr("secret_command_failed", "secret_command", fmt.Sprintf("exit status %d; %s", code, detail), recovery)
	}
	out := bytes.TrimSuffix(bytes.TrimSuffix(stdout.Bytes(), []byte("\n")), []byte("\r"))
	if len(out) == 0 {
		return nil, authErr("secret_command_empty", "secret_command", "the secret command printed nothing", recovery)
	}
	return out, nil
}

type FileSecret struct{ Path string }

func (FileSecret) Name() string          { return "file" }
func (f FileSecret) Put(pw []byte) error { return config.WriteFileAtomic(f.Path, pw) }
func (f FileSecret) Password(context.Context) ([]byte, error) {
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, authErr("file_secret_missing", "file", err.Error(), "run `taiga auth login --insecure-storage` again")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, authErr("file_secret_insecure_permissions", "file", fmt.Sprintf("%s has mode %v", f.Path, info.Mode().Perm()), "chmod 600 the file")
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, authErr("file_secret_missing", "file", err.Error(), "")
	}
	return b, nil
}

// FirstOf tries each source in order.
type FirstOf []SecretSource

func (f FirstOf) Name() string {
	names := make([]string, len(f))
	for i, s := range f {
		names[i] = s.Name()
	}
	return strings.Join(names, ",")
}
func (f FirstOf) Password(ctx context.Context) ([]byte, error) {
	var last error = authErr("auth_no_source", "config", "no secret source configured", "run `taiga auth login`")
	for _, s := range f {
		pw, err := s.Password(ctx)
		if err == nil {
			return pw, nil
		}
		last = err
	}
	return nil, last
}
