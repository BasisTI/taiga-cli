package cli

import (
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
