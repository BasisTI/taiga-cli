package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// cobraScript is the script Cobra itself generates for shell, debug log included.
func cobraScript(t *testing.T, shell string, desc bool) string {
	t.Helper()
	root := (&App{}).root()
	var b bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, desc)
	case "zsh":
		if desc {
			err = root.GenZshCompletion(&b)
		} else {
			err = root.GenZshCompletionNoDesc(&b)
		}
	case "fish":
		err = root.GenFishCompletion(&b, desc)
	case "powershell":
		if desc {
			err = root.GenPowerShellCompletionWithDesc(&b)
		} else {
			err = root.GenPowerShellCompletion(&b)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// generatedScript is what `taiga completion shell` prints.
func generatedScript(t *testing.T, shell string, desc bool) string {
	t.Helper()
	args := []string{"completion", shell}
	if !desc {
		args = append(args, "--no-descriptions")
	}
	out, stderr, code := runIn(t, nil, "", args...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, stderr)
	}
	return out
}

var (
	debugCall = regexp.MustCompile(`^\s*__taiga_debug( .*)?$`)
	noopLine  = regexp.MustCompile(`^\s*(:|true)?$`)
)

// withoutDebug drops the shell's debug writer, its calls and the lines a no-op or blank, so
// two scripts compare on everything else.
func withoutDebug(script string) []string {
	out := []string{}
	writer := false
	for _, line := range strings.Split(script, "\n") {
		switch {
		case line == "__taiga_debug()" || strings.HasPrefix(line, "function __taiga_debug"):
			writer = true
		case writer:
			writer = line != "}" && line != "end"
		case debugCall.MatchString(line), noopLine.MatchString(line):
		default:
			out = append(out, line)
		}
	}
	return out
}

// The scripts carry no debug log of their own: no writer, no call, no BASH_COMP_DEBUG_FILE.
// Everything else is Cobra's script, line for line.
func TestCompletionScriptsHaveNoShellDebug(t *testing.T) {
	for _, shell := range completionShells {
		for _, desc := range []bool{true, false} {
			got := generatedScript(t, shell, desc)
			if strings.Contains(got, "__taiga_debug") || strings.Contains(got, "BASH_COMP_DEBUG_FILE") {
				t.Errorf("%s (descriptions %v) still logs to BASH_COMP_DEBUG_FILE", shell, desc)
			}
			want := withoutDebug(cobraScript(t, shell, desc))
			if g := withoutDebug(got); strings.Join(g, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s (descriptions %v) differs from Cobra's beyond the debug log", shell, desc)
			}
		}
	}
}

// buildTaiga builds the binary the scripts call, as `taiga` in a directory of its own.
func buildTaiga(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "taiga"), "../../cmd/taiga")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return dir
}

// No word is a glob: zsh and fish refuse an unmatched one before calling the binary.
//
// Each harness loads the script, completes `taiga WORDS...` through the shell's own entry
// point and prints the candidates. $SCRIPT is the script, $BIN the binary; WORDS are the
// arguments, the last one being completed (empty after a space).
var completionHarness = map[string]struct {
	shell string
	args  func(harness string) []string
	code  string
}{
	"bash": {"bash", func(h string) []string { return []string{"--norc", "--noprofile", "-c", h, "bash"} }, `
ARGS=("$@")
source "$SCRIPT"
_init_completion() { words=("$BIN" "${ARGS[@]}"); cword=$(( ${#words[@]} - 1 )); cur=${words[cword]}; prev=${words[cword-1]}; }
compopt() { :; }
COMP_TYPE=9
__start_taiga
printf '%s\n' "${COMPREPLY[@]}"
`},
	"zsh": {"zsh", func(h string) []string { return []string{"-f", "-c", h, "zsh"} }, `
ARGS=("$@")
compdef() { :; }
source "$SCRIPT"
compadd() { print -r -- "compadd $*"; }
_describe() { print -r -- "describe $* :: ${(j:|:)completions}"; return 0; }
_arguments() { print -r -- "arguments $*"; }
words=("$BIN" "${ARGS[@]}")
CURRENT=${#words}
_taiga
`},
	"fish": {"fish", func(h string) []string { return []string{"--no-config", "-c", h, "--"} }, `
source $SCRIPT
set -l words (string escape -- $argv)
if test -z "$argv[-1]"
    set words $words[1..-2] ''
end
complete -C (string join ' ' -- taiga $words)
`},
	"powershell": {"pwsh", func(h string) []string { return []string{"-NoProfile", "-NonInteractive", "-Command", h} }, `
. $env:SCRIPT
$line = 'taiga ' + ($env:WORDS -split "\n" -join ' ')
(TabExpansion2 -inputScript $line -cursorColumn $line.Length).CompletionMatches | ForEach-Object { $_.CompletionText }
`},
}

// complete runs the harness of shell over script with words and returns the candidates and
// the debug file.
func complete(t *testing.T, shell, bin, script string, words ...string) (out, debug string) {
	t.Helper()
	h := completionHarness[shell]
	dir := t.TempDir()
	path, log := filepath.Join(dir, "script"), filepath.Join(dir, "debug.log")
	if shell == "powershell" {
		path += ".ps1" // dot-sourcing runs only a .ps1
	}
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	args := h.args(h.code)
	if shell != "powershell" {
		args = append(args, words...)
	}
	cmd := exec.Command(h.shell, args...)
	cmd.Dir = t.TempDir() // empty: file completion lists neither the script nor the log
	cmd.Env = append(os.Environ(), "SCRIPT="+path, "BIN="+filepath.Join(bin, "taiga"), "BASH_COMP_DEBUG_FILE="+log,
		"WORDS="+strings.Join(words, "\n"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "LC_ALL=C.UTF-8")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %q: %v\n%s%s", shell, words, err, stdout.String(), stderr.String())
	}
	b, _ := os.ReadFile(log)
	return stdout.String(), string(b)
}

// With BASH_COMP_DEBUG_FILE set, completing a command line with a token, a control and a bidi
// character leaves only the binary's redacted log in the file; the candidates are those of
// Cobra's own script. A shell that is not installed is skipped.
func TestCompletionScriptsKeepTokensOutOfTheDebugFile(t *testing.T) {
	bin := buildTaiga(t)
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(completionHarness[shell].shell); err != nil {
				if shell == "bash" {
					t.Fatal("bash is required")
				}
				t.Skipf("%s is not installed", completionHarness[shell].shell)
			}
			script := generatedScript(t, shell, true)
			_, debug := complete(t, shell, bin, script, "nope", "token=SWEEPSECRET", "a\u202eb", "x\x01y", "")
			if strings.Contains(debug, "SWEEPSECRET") || strings.ContainsAny(debug, "\u202e\x01") {
				t.Errorf("debug file leaked:\n%q", debug)
			}
			if !strings.Contains(debug, "[Debug] [Error] unable to find a command for arguments: [nope token=…") {
				t.Errorf("the binary's own log is gone:\n%q", debug)
			}
			for _, words := range [][]string{{"st"}, {"story", ""}, {"story", "l"}, {"story", "list", "--outp"}, {"story", "list", "--"}, {"completion", ""}, {"attachment", "upload", "--t"}} {
				got, _ := complete(t, shell, bin, script, words...)
				want, _ := complete(t, shell, bin, cobraScript(t, shell, true), words...)
				t.Logf("%q: %q", words, got)
				if got != want || got == "" {
					t.Errorf("%q: candidates %q, Cobra's script gives %q", words, got, want)
				}
			}
		})
	}
}
