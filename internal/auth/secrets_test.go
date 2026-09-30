package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestEnvPassword(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "pw")
	_ = os.WriteFile(pf, []byte("from-file\n"), 0o600)
	e := EnvPassword{Env: func(k string) string { return map[string]string{"TAIGA_PASSWORD_FILE": pf}[k] }}
	pw, err := e.Password(context.Background())
	if err != nil || string(pw) != "from-file" || !e.Configured() {
		t.Fatalf("%q %v", pw, err)
	}
	e = EnvPassword{Env: func(k string) string {
		return map[string]string{"TAIGA_PASSWORD": "direct", "TAIGA_PASSWORD_FILE": pf}[k]
	}}
	if pw, _ := e.Password(context.Background()); string(pw) != "direct" {
		t.Fatalf("TAIGA_PASSWORD must win: %q", pw)
	}
}

func TestCommandSecretSuccessAndErrors(t *testing.T) {
	ctx := context.Background()
	pw, err := CommandSecret{Args: []string{"/bin/sh", "-c", "printf 'segredo\\n'"}}.Password(ctx)
	if err != nil || string(pw) != "segredo" {
		t.Fatalf("%q %v", pw, err)
	}
	_, err = CommandSecret{Args: []string{"/bin/sh", "-c", "echo 'gpg: decryption failed: Inappropriate ioctl for device' >&2; exit 2"}}.Password(ctx)
	e := output.AsError(err)
	if e.Code != "secret_command_failed" || !strings.Contains(e.Cause, "exit status 2") || !strings.Contains(e.Cause, "Inappropriate ioctl") || !strings.Contains(e.Recovery, "GPG_TTY") {
		t.Fatalf("%+v", e)
	}
	_, err = CommandSecret{Args: []string{"/nonexistent/helper"}}.Password(ctx)
	if output.AsError(err).Code != "secret_command_not_found" {
		t.Fatalf("%v", err)
	}
	_, err = CommandSecret{Args: []string{"/bin/true"}}.Password(ctx)
	if output.AsError(err).Code != "secret_command_empty" {
		t.Fatalf("%v", err)
	}
}

func TestCommandSecretTimeout(t *testing.T) {
	old := commandTimeout
	commandTimeout = 200 * time.Millisecond
	defer func() { commandTimeout = old }()
	start := time.Now()
	_, err := CommandSecret{Args: []string{"/bin/sh", "-c", "sleep 5"}}.Password(context.Background())
	if output.AsError(err).Code != "secret_command_timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
}

func TestFileSecretRequiresPrivateMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets", "ref")
	f := FileSecret{Path: p}
	if err := f.Put([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	if pw, err := f.Password(context.Background()); err != nil || string(pw) != "pw" {
		t.Fatalf("%q %v", pw, err)
	}
	_ = os.Chmod(p, 0o644)
	if _, err := f.Password(context.Background()); output.AsError(err).Code != "file_secret_insecure_permissions" {
		t.Fatalf("%v", err)
	}
}

type failing struct{ name string }

func (f failing) Name() string { return f.name }
func (f failing) Password(context.Context) ([]byte, error) {
	return nil, &output.Error{Code: "x_" + f.name, Exit: 3}
}

func TestFirstOf(t *testing.T) {
	pw, err := FirstOf{failing{"a"}, CommandSecret{Args: []string{"/bin/echo", "ok"}}}.Password(context.Background())
	if err != nil || string(pw) != "ok" {
		t.Fatalf("%q %v", pw, err)
	}
	_, err = FirstOf{failing{"a"}, failing{"b"}}.Password(context.Background())
	if output.AsError(err).Code != "x_b" {
		t.Fatalf("%v", err)
	}
}

// Review focus: gpg stuck on pinentry without a TTY must end with the GPG_TTY hint.
func TestCommandSecretTimeoutKeepsPinentryHint(t *testing.T) {
	old := commandTimeout
	commandTimeout = 200 * time.Millisecond
	defer func() { commandTimeout = old }()
	_, err := CommandSecret{Args: []string{"/bin/sh", "-c", "echo 'gpg: pinentry: no tty' >&2; sleep 5"}}.Password(context.Background())
	e := output.AsError(err)
	if e.Code != "secret_command_timeout" || e.Exit != 3 || !strings.Contains(e.Cause, "pinentry") || !strings.Contains(e.Recovery, "GPG_TTY") {
		t.Fatalf("%+v", e)
	}
}
