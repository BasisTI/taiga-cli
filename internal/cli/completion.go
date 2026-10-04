package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

// completeRedacted makes Cobra's hidden __complete command see the command line redacted and
// escaped, as presented elsewhere.
//
// Cobra reports a completion error (unknown command, unknown flag, bad flag value) with
// CompErrorln, which writes to os.Stderr and appends to $BASH_COMP_DEBUG_FILE on its own: no
// writer of the command reaches it, so the lineRedactor of wireOutput never sees that text.
// The message only joins fixed text, the names of commands and flags and the arguments typed,
// so redacting the arguments before Cobra reads them is what keeps a token or a control
// character out of both. Completion never calls Taiga, and TestOutputGoesThroughPresent keeps
// completion functions (which could print server text) out of the CLI.
//
// An argument changes only when it carries a token or an unsafe rune, and such an argument
// never names a command or a flag, so the candidates stay the same; the escape drops Quote's
// outer quotes so that `--flag…` still reads as a flag.
func completeRedacted(cmd *cobra.Command) {
	run := cmd.Run
	cmd.Run = func(c *cobra.Command, args []string) {
		safe := make([]string, len(args))
		for i, s := range args {
			safe[i] = completionArg(s)
		}
		run(c, safe)
	}
}

// completionArg is s redacted, with each unsafe rune escaped and no quotes around it. Only
// what Quote quoted loses its quotes: a redacted value can take the unsafe rune with it.
func completionArg(s string) string {
	r := taiga.RedactTokens(s)
	if q := output.Quote(r); q != r {
		return q[1 : len(q)-1]
	}
	return r
}

// completionCmd gives Cobra's `completion bash|zsh|fish|powershell` (its help and flags kept)
// scripts whose own debug log writes nothing. That log appends the command line as typed,
// tokens included, to $BASH_COMP_DEBUG_FILE; the binary's log there stays, redacted by
// completeRedacted.
func (a *App) completionCmd(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() != "completion" {
			continue
		}
		for _, sh := range c.Commands() {
			shell := sh.Name()
			sh.RunE = func(cmd *cobra.Command, _ []string) error {
				noDesc, _ := cmd.Flags().GetBool("no-descriptions")
				s, err := completionScript(cmd.Root(), shell, !noDesc)
				if err != nil {
					return err
				}
				return a.writeScript(cmd, s)
			}
		}
	}
}

// completionScript is Cobra's script for shell with its debug writer redefined to do nothing.
func completionScript(root *cobra.Command, shell string, desc bool) (string, error) {
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
	default:
		return "", fmt.Errorf("no completion script for %s", shell)
	}
	if err != nil {
		return "", err
	}
	return withQuietDebug(b.String(), "__"+root.Name()+"_debug", shell)
}

// quietWriter redefines the debug writer (%s) as a function that does nothing and returns 0,
// as the writer itself does when the variable is unset.
var quietWriter = map[string]string{"bash": "%s() { :; }", "zsh": "%s() { :; }", "fish": "function %s; end", "powershell": "function %s { }"}

// declaration finds a function declared on a line, at any indent: "name()" and "function
// name" (bash, zsh), "function name" (fish), "function name", "filter name" and
// "[scriptblock]${name} =" (PowerShell).
var declaration = regexp.MustCompile(`^\s*(?:(?:function|filter)\s+([^\s(){};]+)|([A-Za-z_][\w:.-]*)\s*\(\)|\[scriptblock\]\$\{([^}]+)\}\s*=)`)

// declared is the function a line declares, or "".
func declared(line string) string {
	m := declaration.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1] + m[2] + m[3]
}

// withQuietDebug is script with writer redefined to do nothing, or an error and no script.
// Nothing of Cobra's script is removed or changed: the redefinition is added before the first
// function declared at the margin after writer's (last) declaration, above the comment lines
// that go with that function. Writer's body has ended there, and nothing has run yet: zsh
// autoloaded from fpath runs the file on first use and calls the completion at its end, and
// fish completes once while loading, so the end of the script would be too late. Every call
// then goes to the redefinition, PowerShell's included (same scope as the script's own
// functions). Without writer's declaration, or with no declaration after it, the script is
// refused.
func withQuietDebug(script, writer, shell string) (string, error) {
	lines := strings.SplitAfter(script, "\n")
	last := -1
	for i, line := range lines {
		if declared(strings.TrimRight(line, "\n")) == writer {
			last = i
		}
	}
	if last < 0 {
		return "", fmt.Errorf("the %s completion script does not declare %s", shell, writer)
	}
	for i := last + 1; i < len(lines); i++ {
		text := strings.TrimRight(lines[i], "\n")
		if declared(text) == "" || text != strings.TrimLeft(text, " \t") {
			continue
		}
		for i > last+1 && strings.HasPrefix(lines[i-1], "#") {
			i-- // above the comment that goes with that function
		}
		quiet := "# taiga: " + writer + " above appends the command line as typed, tokens included, to\n" +
			"# $BASH_COMP_DEBUG_FILE. Redefined here, before any completion runs, it writes nothing.\n" +
			fmt.Sprintf(quietWriter[shell], writer) + "\n\n"
		return strings.Join(lines[:i], "") + quiet + strings.Join(lines[i:], ""), nil
	}
	return "", fmt.Errorf("the %s completion script declares nothing after %s", shell, writer)
}
