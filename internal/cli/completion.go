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
// scripts without the shell's own debug log. That log appends the command line as typed,
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

// completionScript is Cobra's script for shell without its debug writer and the calls to it.
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
	return withoutShellDebug(b.String(), "__"+root.Name()+"_debug", shellNoop[shell])
}

// shellNoop replaces each call to the debug writer, so a block never ends up empty and the
// status stays 0, as the writer's own when the variable is unset. PowerShell needs none.
var shellNoop = map[string]string{"bash": ":", "zsh": ":", "fish": "true", "powershell": ""}

// debugArgs is the one double-quoted argument of a call to the debug writer, and nothing after.
var debugArgs = regexp.MustCompile(`^\s+"(?:[^"\\]|\\.)*"$`)

// withoutShellDebug is script without the debug writer and with each call to it turned into
// noop, or an error and no script. Cutting goes by the text (dropShellDebug), which a change
// of Cobra's format could fool; what comes out is then checked against script without
// relying on that format (onlyDebugDropped): every function but the writer is still
// declared, and nothing else changed. A script that fails the check is refused.
func withoutShellDebug(script, writer, noop string) (string, error) {
	out, err := dropShellDebug(script, writer, noop)
	if err == nil {
		err = onlyDebugDropped(script, out, writer, noop)
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

// dropShellDebug drops the definition of writer, from its first line to the "}" or "end"
// that closes it at the margin, and turns each call into noop. A line at the margin inside
// the writer (other than its braces) is refused; a call must be a line of its own with at
// most one quoted argument, since anything after it would go with the line. Anything left
// that names the writer or the variable is an error.
func dropShellDebug(script, writer, noop string) (string, error) {
	var b strings.Builder
	inWriter := false
	for _, line := range strings.SplitAfter(script, "\n") {
		text := strings.TrimRight(line, "\n")
		trimmed := strings.TrimSpace(text)
		switch {
		case inWriter:
			// The body is indented: a line at the margin closes the writer or is not its own.
			margin := text != "" && text == strings.TrimLeft(text, " \t")
			if margin && text != "{" && text != "}" && text != "end" {
				return "", fmt.Errorf("the %s writer of the completion script has no closing line at the margin", writer)
			}
			inWriter = text != "}" && text != "end"
		case text == writer+"()" || text == "function "+writer || text == "function "+writer+" {":
			inWriter = true
		case trimmed == writer || strings.HasPrefix(trimmed, writer+" ") || strings.HasPrefix(trimmed, writer+"\t"):
			if trimmed != writer && !debugArgs.MatchString(trimmed[len(writer):]) {
				return "", fmt.Errorf("the %s completion script calls %s next to another statement: %q", writer, writer, trimmed)
			}
			if noop != "" {
				b.WriteString(text[:len(text)-len(strings.TrimLeft(text, " \t"))] + noop + "\n")
			}
		default:
			b.WriteString(line)
		}
	}
	out := b.String()
	if inWriter || strings.Contains(out, writer) || strings.Contains(out, "BASH_COMP_DEBUG_FILE") {
		return "", fmt.Errorf("the %s completion script still logs to BASH_COMP_DEBUG_FILE", writer)
	}
	return out, nil
}

// declaration finds a function declared on a line, in any of the four scripts and at any
// indent: "name()" and "function name" (bash, zsh), "function name" (fish), "function name",
// "filter name" and "[scriptblock]${name} =" (PowerShell).
var declaration = regexp.MustCompile(`^\s*(?:(?:function|filter)\s+([^\s(){}]+)|([A-Za-z_][\w:.-]*)\s*\(\)|\[scriptblock\]\$\{([^}]+)\}\s*=)`)

// declared is the function a line declares, or "".
func declared(line string) string {
	m := declaration.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1] + m[2] + m[3]
}

// onlyDebugDropped checks out against script, line by line: each line is kept as is, or is a
// call to writer that became noop (dropped when noop is ""), or belongs to one run of lines
// that starts where writer is declared, ends on "}" or "end" and declares nothing else. It
// also checks that every function script declares, writer aside, is still declared in out.
func onlyDebugDropped(script, out, writer, noop string) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("the completion script changed beyond its debug log: "+format, args...)
	}
	count := func(text string) map[string]int {
		names := map[string]int{}
		for _, line := range strings.Split(text, "\n") {
			if name := declared(line); name != "" {
				names[name]++
			}
		}
		return names
	}
	want, got := count(script), count(out)
	delete(want, writer)
	for name, n := range want {
		if got[name] != n {
			return fail("%s is declared %d times, %d before", name, got[name], n)
		}
	}
	if len(got) != len(want) {
		return fail("it declares %d functions, %d before", len(got), len(want))
	}
	o, f := strings.Split(script, "\n"), strings.Split(out, "\n")
	i, j, runs := 0, 0, 0
	for i < len(o) {
		trimmed := strings.TrimSpace(o[i])
		switch {
		case j < len(f) && o[i] == f[j]:
			i, j = i+1, j+1
		case trimmed == writer || strings.HasPrefix(trimmed, writer+" ") || strings.HasPrefix(trimmed, writer+"\t"):
			if noop != "" {
				if j >= len(f) || f[j] != o[i][:len(o[i])-len(strings.TrimLeft(o[i], " \t"))]+noop {
					return fail("line %d: a call to %s did not become %q", i+1, writer, noop)
				}
				j++
			}
			i++
		case declared(o[i]) == writer && runs == 0:
			runs++
			k := i + 1
			for k < len(o) && (j >= len(f) || o[k] != f[j]) {
				if name := declared(o[k]); name != "" {
					return fail("line %d: %s went with the writer", k+1, name)
				}
				k++
			}
			if last := strings.TrimSpace(o[k-1]); last != "}" && last != "end" {
				return fail("line %d: the writer does not end on a closing line", k)
			}
			i = k
		default:
			return fail("line %d (%q) is missing", i+1, o[i])
		}
	}
	if j != len(f) {
		return fail("%d lines were added", len(f)-j)
	}
	return nil
}
