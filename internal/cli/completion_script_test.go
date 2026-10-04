package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

// quietBlock is what `taiga completion shell` adds to Cobra's script, and all it adds.
func quietBlock(shell string) string {
	def := map[string]string{"bash": "__taiga_debug() { :; }", "zsh": "__taiga_debug() { :; }",
		"fish": "function __taiga_debug; end", "powershell": "function __taiga_debug { }"}[shell]
	return "# taiga: __taiga_debug above appends the command line as typed, tokens included, to\n" +
		"# $BASH_COMP_DEBUG_FILE. Redefined here, before any completion runs, it writes nothing.\n" + def + "\n\n"
}

// The scripts are Cobra's with one block added after the debug writer's definition, which
// redefines the writer to do nothing; nothing else is added, removed or changed.
func TestCompletionScriptsOnlyAddTheQuietWriter(t *testing.T) {
	for _, shell := range completionShells {
		for _, desc := range []bool{true, false} {
			got, cobra := generatedScript(t, shell, desc), cobraScript(t, shell, desc)
			at := strings.Index(got, quietBlock(shell))
			if at < 0 || got[:at]+got[at+len(quietBlock(shell)):] != cobra {
				t.Errorf("%s (descriptions %v) is not Cobra's script plus the quiet writer", shell, desc)
				continue
			}
			if first := strings.Index(cobra, "__taiga_debug \""); at > first || at < strings.Index(cobra, "__taiga_debug") {
				t.Errorf("%s (descriptions %v): the quiet writer at %d is not between the writer and its first call (%d)", shell, desc, at, first)
			}
		}
	}
}

// Without the writer's declaration under the expected name, or with nothing declared after
// it, the script is refused: an error and no script.
func TestCompletionScriptRefusedWithoutTheWriter(t *testing.T) {
	cobra := cobraScript(t, "bash", true)
	for name, script := range map[string]string{
		"renamed writer":  strings.ReplaceAll(cobra, "__taiga_debug", "__taiga_log"),
		"no writer":       "_start() { :; }\n",
		"nothing after":   "__taiga_debug()\n{\n    :\n}\n",
		"declared inside": "f() {\n    __taiga_debug() { :; }\n}\n",
	} {
		if got, err := withQuietDebug(script, "__taiga_debug", "bash"); err == nil || got != "" {
			t.Errorf("%s: error %v, got %q", name, err, got)
		}
	}
}

// statusCheck loads $SCRIPT in a shell, calls the writer after a failure and prints the status:
// the redefinition returns 0, as the writer itself.
var statusCheck = map[string][]string{
	"bash":       {"bash", "--norc", "--noprofile", "-c", `source "$SCRIPT"; false; __taiga_debug "x"; echo "status=$?"`},
	"zsh":        {"zsh", "-f", "-c", `compdef() { :; }; source "$SCRIPT"; false; __taiga_debug "x"; echo "status=$?"`},
	"fish":       {"fish", "--no-config", "-c", `source $SCRIPT; false; __taiga_debug "x"; echo "status=$status"`},
	"powershell": {"pwsh", "-NoProfile", "-NonInteractive", "-Command", `. $env:SCRIPT; $out = __taiga_debug "x"; if ($? -and $null -eq $out) { "status=0" } else { "status=1" }`},
}

// With BASH_COMP_DEBUG_FILE set, the redefined writer returns 0, prints nothing and writes
// nothing.
func TestCompletionScriptsQuietWriter(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			c := statusCheck[shell]
			requireShell(t, c[0])
			dir := t.TempDir()
			path, log := filepath.Join(dir, "script.ps1"), filepath.Join(dir, "debug.log")
			if err := os.WriteFile(path, []byte(generatedScript(t, shell, true)), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "SCRIPT="+path, "BASH_COMP_DEBUG_FILE="+log)
			out, err := cmd.Output()
			if err != nil || string(out) != "status=0\n" {
				t.Errorf("%v: %q", err, out)
			}
			if b, err := os.ReadFile(log); err == nil {
				t.Errorf("the writer wrote %q", b)
			}
		})
	}
}

// requireShell skips the test when exe is not installed, unless exe is bash or listed in
// TAIGA_TEST_SHELLS (comma-separated, as the CI sets it): then it fails, so no shell goes
// untested in silence.
func requireShell(t *testing.T, exe string) {
	t.Helper()
	if _, err := exec.LookPath(exe); err == nil {
		return
	}
	for _, want := range append([]string{"bash"}, strings.Split(os.Getenv("TAIGA_TEST_SHELLS"), ",")...) {
		if strings.TrimSpace(want) == exe {
			t.Fatalf("%s is required and not installed", exe)
		}
	}
	t.Skipf("%s is not installed", exe)
}

// parsers check a script without running it.
var parsers = map[string][]string{
	"bash":       {"bash", "-n"},
	"zsh":        {"zsh", "-n"},
	"fish":       {"fish", "--no-execute"},
	"powershell": {"pwsh", "-NoProfile", "-NonInteractive", "-Command", "$e = $null; $null = [System.Management.Automation.Language.Parser]::ParseFile($env:PARSE_FILE, [ref]$null, [ref]$e); if ($e) { $e; exit 1 }"},
}

// Each generated script parses in its shell; a shell that is not installed is skipped.
func TestCompletionScriptsParse(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			p := parsers[shell]
			requireShell(t, p[0])
			for _, desc := range []bool{true, false} {
				path := filepath.Join(t.TempDir(), "script.ps1")
				if err := os.WriteFile(path, []byte(generatedScript(t, shell, desc)), 0o600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(p[0], append(p[1:], path)...) // pwsh -Command ignores the path and reads PARSE_FILE
				cmd.Env = append(os.Environ(), "PARSE_FILE="+path)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("descriptions %v: %v\n%s", desc, err, out)
				}
			}
		})
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
	// zsh as installed: the script is _taiga in fpath, autoloaded, and the file itself runs on
	// the first completion.
	"zsh-autoload": {"zsh", func(h string) []string { return []string{"-f", "-c", h, "zsh"} }, `
ARGS=("$@")
dir=$(mktemp -d)
cp "$SCRIPT" "$dir/_taiga"
fpath=("$dir" $fpath)
compdef() { :; }
compadd() { print -r -- "compadd $*"; }
_describe() { print -r -- "describe $* :: ${(j:|:)completions}"; return 0; }
_arguments() { print -r -- "arguments $*"; }
autoload -U _taiga
words=("$BIN" "${ARGS[@]}")
CURRENT=${#words}
_taiga
rm -r "$dir"
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
// character leaves only the binary's redacted log in the file, no line of the shell's; the candidates are those of
// Cobra's own script. A shell that is not installed is skipped.
func TestCompletionScriptsKeepTokensOutOfTheDebugFile(t *testing.T) {
	bin := buildTaiga(t)
	for _, harness := range []string{"bash", "zsh", "zsh-autoload", "fish", "powershell"} {
		shell := strings.TrimSuffix(harness, "-autoload")
		t.Run(harness, func(t *testing.T) {
			requireShell(t, completionHarness[harness].shell)
			for _, desc := range []bool{true, false} {
				script := generatedScript(t, shell, desc)
				_, debug := complete(t, harness, bin, script, "nope", "token=SWEEPSECRET", "a\u202eb", "x\x01y", "")
				if strings.Contains(debug, "SWEEPSECRET") || strings.ContainsAny(debug, "\u202e\x01") {
					t.Errorf("descriptions %v: debug file leaked:\n%q", desc, debug)
				}
				for _, line := range strings.Split(strings.TrimSuffix(debug, "\n"), "\n") {
					if !strings.HasPrefix(line, "[Debug] ") {
						t.Errorf("descriptions %v: the shell wrote %q", desc, line)
					}
				}
				if !strings.Contains(debug, "[Debug] [Error] unable to find a command for arguments: [nope token=…") {
					t.Errorf("descriptions %v: the binary's own log is gone:\n%q", desc, debug)
				}
			}
			for _, desc := range []bool{true, false} {
				script, cobra := generatedScript(t, shell, desc), cobraScript(t, shell, desc)
				for _, words := range [][]string{{"st"}, {"story", ""}, {"story", "l"}, {"story", "list", "--outp"}, {"story", "list", "--"}, {"completion", ""}, {"attachment", "upload", "--t"}} {
					got, _ := complete(t, harness, bin, script, words...)
					want, _ := complete(t, harness, bin, cobra, words...)
					t.Logf("descriptions %v, %q: %q", desc, words, got)
					if got != want || got == "" {
						t.Errorf("descriptions %v, %q: candidates %q, Cobra's script gives %q", desc, words, got, want)
					}
				}
			}
		})
	}
}
