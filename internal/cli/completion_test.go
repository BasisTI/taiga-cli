package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runComplete runs `taiga req args...` (req is __complete or __completeNoDesc) the way the
// shell does, with BASH_COMP_DEBUG_FILE set, and returns stdout, everything written to stderr
// (App.Err and the process stderr, where Cobra's CompErrorln writes) and the debug file.
func runComplete(t *testing.T, env map[string]string, req string, args ...string) (out, stderr, debug string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "comp.log")
	t.Setenv("BASH_COMP_DEBUG_FILE", path)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	read := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		read <- string(b)
	}()
	stdout, appErr, code := runIn(t, env, "", append([]string{req}, args...)...)
	os.Stderr = saved
	_ = w.Close()
	process := <-read
	if code != 0 {
		t.Fatalf("__complete %q: exit %d: %s%s", args, code, appErr, process)
	}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return stdout, appErr + process, string(b)
}

// Completion errors echo the command line (an unknown command, a flag name, a flag value):
// stderr and the debug file get them redacted and escaped, like every other command.
func TestCompletionHidesTokens(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string // what still identifies the error
	}{
		{[]string{"nope?token=SWEEPSECRET", ""}, "unable to find a command"},
		{[]string{"nope", sweepURL, ""}, "unable to find a command"},
		{[]string{"story", "list", "--bogus?access_token=SWEEPSECRET=1", ""}, "unknown flag"},
		{[]string{"story", "list", "--url", "x", "--bogus=" + sweepURL, ""}, "unknown flag"},
		{[]string{"attachment", "upload", "--task=token=SWEEPSECRET", ""}, "invalid argument"},
		// Redacted whole, the argument is no flag any more: no error, and nothing printed.
		{[]string{"story", "list", "--access_token%3DSWEEPSECRET="}, ""},
		{[]string{"story", "list", "--a\u202eb="}, `does not support flag 'a\u202eb'`},
		{[]string{"story", "list", "--a\u202eb=1", ""}, `--a\u202eb`},
		{[]string{"nope\nforged", ""}, `nope\nforged`},
	} {
		for _, req := range []string{"__complete", "__completeNoDesc"} {
			out, stderr, debug := runComplete(t, nil, req, c.args...)
			for name, text := range map[string]string{"stdout": out, "stderr": stderr, "debug file": debug} {
				if strings.Contains(text, "SWEEPSECRET") || strings.ContainsAny(text, "\u202e") || strings.Contains(text, "\nforged") {
					t.Errorf("%s %q: %s leaked: %q", req, c.args, name, text)
				}
			}
			if !strings.Contains(stderr, c.want) || !strings.Contains(debug, c.want) {
				t.Errorf("%s %q: the error is gone (want %q)\nstderr: %q\ndebug: %q", req, c.args, c.want, stderr, debug)
			}
		}
	}
}

// Completion stays what Cobra computes: redaction only touches arguments that carry a token
// or an unsafe rune, which never name a command or a flag.
func TestCompletionCandidatesUnchanged(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"story", "l"}, []string{"list\t", ":4"}},
		{[]string{"story", "list", "--url", sweepURL, "--outp"}, []string{"--output\t", ":4"}},
		{[]string{"story", "list", "--url=" + sweepURL, "--outp"}, []string{"--output\t", ":4"}},
		{[]string{"story", "list", "--a\u202eb", ""}, []string{":0"}},
	} {
		out, _, _ := runComplete(t, nil, "__complete", c.args...)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%q: want %q in %q", c.args, w, out)
			}
		}
	}
}

// Completion never calls Taiga, so no server text can reach it: nothing in the CLI registers
// a completion function (TestOutputGoesThroughPresent keeps it that way).
func TestCompletionNeverCallsTaiga(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id": 1, "name": "` + sweepURL + `", "slug": "` + sweepURL + `"}]`))
	})
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok", "TAIGA_PROJECT": "infra-2025"}
	for _, args := range [][]string{{""}, {"story", "get", ""}, {"story", "list", "--project", ""}, {"epic", "link", ""}, {"task", "close", ""}} {
		out, stderr, debug := runComplete(t, env, "__complete", args...)
		if len(*calls) != 0 || strings.Contains(out+stderr+debug, "SWEEPSECRET") {
			t.Fatalf("%q: %d calls\n%s%s%s", args, len(*calls), out, stderr, debug)
		}
	}
}
