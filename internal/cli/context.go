package cli

import (
	"context"
	"net/http"

	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// RunContext is the resolved configuration for one command run.
type RunContext struct {
	Paths config.Paths
	File  config.File
	Ctx   config.Context
}

func (a *App) runContext() (*RunContext, error) {
	paths, err := config.DefaultPaths(a.Env)
	if err != nil {
		return nil, err
	}
	file, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	ctx, err := config.Resolve(config.Inputs{FlagURL: a.flagURL, FlagProject: a.flagProject, Env: a.Env, Cwd: a.Cwd, File: file, ConfigPath: paths.ConfigFile})
	if err != nil {
		return nil, err
	}
	return &RunContext{Paths: paths, File: file, Ctx: ctx}, nil
}

// envTokenSource is the phase-1 bootstrap source; task 12 replaces App.TokenSource with the auth resolver.
func envTokenSource(a *App) func(context.Context, *RunContext) (taiga.TokenSource, error) {
	return func(context.Context, *RunContext) (taiga.TokenSource, error) {
		tok := a.Env("TAIGA_TOKEN")
		if tok == "" {
			return nil, &output.Error{Code: "auth_no_source", Source: "env", Cause: "TAIGA_TOKEN is not set", Recovery: "set TAIGA_TOKEN", Exit: output.ExitAuth}
		}
		typ := a.Env("TAIGA_TOKEN_TYPE")
		if typ == "" {
			typ = "Bearer"
		}
		return taiga.StaticToken{Type: typ, Value: tok}, nil
	}
}

func (a *App) client(ctx context.Context, rc *RunContext) (*taiga.Client, error) {
	ts, err := a.TokenSource(ctx, rc)
	if err != nil {
		return nil, err
	}
	return taiga.New(rc.Ctx.URL.Value, ts, taiga.WithHTTPClient(a.httpClient())), nil
}

func (a *App) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: httpTimeout}
}
