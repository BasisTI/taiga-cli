// Package cli wires the taiga command tree.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags "-X github.com/BasisTI/taiga-cli/internal/cli.Version=...".
var Version = "dev"

// App carries the process I/O and environment so commands are testable.
type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Env    func(string) string
	ran    bool
	output string
}

var errUsage = errors.New("usage error")

func (a *App) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "taiga",
		Short:         "Command line client for the Taiga REST API",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(*cobra.Command, []string) {
			a.ran = true
		},
	}
	root.PersistentFlags().StringVar(&a.output, "output", "", "output format: json or text (default: text on a terminal, json otherwise)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", errUsage, err)
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the taiga version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(a.Out, "taiga %s\n", Version)
			return err
		},
	})
	return root
}

// Main runs the CLI and returns the process exit code.
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string) int {
	a := &App{In: in, Out: out, Err: errOut, Env: env}
	root := a.root()
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	err := root.Execute()
	if err == nil {
		return 0
	}
	if errors.Is(err, errUsage) || !a.ran {
		_, _ = fmt.Fprintf(errOut, "error [usage]: %v\n", err)
		return 2
	}
	_, _ = fmt.Fprintf(errOut, "error: %v\n", err)
	return 1
}
