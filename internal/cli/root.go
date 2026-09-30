// Package cli wires the taiga command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
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
	// Cwd is the working directory used to find .taiga.toml; empty means os.Getwd().
	Cwd string
	// HTTP overrides the HTTP client; nil means a client with httpTimeout.
	HTTP *http.Client
	// TokenSource builds the credentials for a run; nil means TAIGA_TOKEN only.
	TokenSource func(context.Context, *RunContext) (taiga.TokenSource, error)

	ran         bool
	output      string
	flagURL     string
	flagProject string
}

const httpTimeout = 30 * time.Second

var errUsage = errors.New("usage error")

func (a *App) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "taiga",
		Short:         "Command line client for the Taiga REST API",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// Flags parsed: from here on errors come from the command, not from cobra.
			a.ran = true
			_, err := output.DetectMode(a.output, a.OutTTY)
			return err
		},
	}
	root.PersistentFlags().StringVar(&a.output, "output", "", "output format: json or text (default: text on a terminal, json otherwise)")
	root.PersistentFlags().StringVar(&a.flagURL, "url", "", "Taiga base URL (overrides TAIGA_URL, .taiga.toml and the config file)")
	root.PersistentFlags().StringVar(&a.flagProject, "project", "", "project slug (overrides TAIGA_PROJECT, .taiga.toml and the config file)")
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
	root.AddCommand(a.apiCmd())
	return root
}

// Main runs the CLI and returns the process exit code.
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string, outTTY bool) int {
	a := &App{In: in, Out: out, Err: errOut, Env: env, OutTTY: outTTY}
	return a.Run(args)
}

// Run executes args against the app and returns the process exit code.
func (a *App) Run(args []string) int {
	if a.TokenSource == nil {
		a.TokenSource = envTokenSource(a)
	}
	if a.Cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			a.Cwd = wd
		}
	}
	root := a.root()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	err := root.Execute()
	if err == nil {
		return output.ExitOK
	}
	return a.finish(err)
}

// finish writes err to stderr and returns its exit code, never 0.
func (a *App) finish(err error) int {
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
	if e.Exit == output.ExitOK {
		e.Exit = output.ExitUnexpected
	}
	_ = output.WriteError(a.Err, mode, e)
	return e.Exit
}
