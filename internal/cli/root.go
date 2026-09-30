// Package cli wires the taiga command tree.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/BasisTI/taiga-cli/internal/output"
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
	OutTTY bool
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
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string, outTTY bool) int {
	a := &App{In: in, Out: out, Err: errOut, Env: env, OutTTY: outTTY}
	root := a.root()
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	err := root.Execute()
	if err == nil {
		return output.ExitOK
	}
	mode, merr := output.DetectMode(a.output, a.OutTTY)
	if merr != nil {
		mode = output.Text
	}
	var e *output.Error
	switch {
	case errors.Is(err, errUsage) || !a.ran:
		e = &output.Error{Code: "usage", Cause: err.Error(), Recovery: "run `taiga --help`", Exit: output.ExitUsage}
	default:
		e = output.AsError(err)
	}
	_ = output.WriteError(errOut, mode, e)
	return e.Exit
}
