package cli

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/auth"
	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// configuredSecret builds the secret source recorded for the host, with env passwords first.
func configuredSecret(env func(string) string, host config.Host, paths config.Paths, ref string) auth.SecretSource {
	var chain auth.FirstOf
	if e := (auth.EnvPassword{Env: env}); e.Configured() {
		chain = append(chain, e)
	}
	switch host.SecretSource {
	case "secret_command":
		chain = append(chain, auth.CommandSecret{Args: host.SecretCommand})
	case "keyring":
		chain = append(chain, auth.Keyring{Ref: ref})
	case "file":
		chain = append(chain, auth.FileSecret{Path: filepath.Join(paths.SecretsDir, ref)})
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}

// envCredentialVars are the variables whose values authenticate on their own.
var envCredentialVars = []string{"TAIGA_TOKEN", "TAIGA_PASSWORD", "TAIGA_PASSWORD_FILE"}

// checkEnvCredentialsTarget refuses to send env credentials to a URL that only a
// .taiga.toml supplied: a cloned repository must not be able to redirect them.
func (a *App) checkEnvCredentialsTarget(rc *RunContext, known bool) error {
	src := rc.Ctx.URL.Source
	if known || src == "flag" || src == "env:TAIGA_URL" || strings.HasPrefix(src, "config:") {
		return nil
	}
	for _, k := range envCredentialVars {
		if a.Env(k) != "" {
			return &output.Error{Code: "auth_untrusted_url", Source: "env", Stage: k, Cause: k + " is not sent to " + rc.Ctx.URL.Value + ": the URL comes only from " + strings.TrimPrefix(src, "file:") + " and is not in the config file", Recovery: "set TAIGA_URL or --url to this URL, or run `taiga auth login --url " + rc.Ctx.URL.Value + "`", Exit: output.ExitAuth}
		}
	}
	return nil
}

func (a *App) resolver(rc *RunContext) (*auth.Resolver, error) {
	host, known := rc.File.Host(rc.Ctx.URL.Value)
	if err := a.checkEnvCredentialsTarget(rc, known); err != nil {
		return nil, err
	}
	username := a.Env("TAIGA_USERNAME")
	if username == "" {
		username = host.Username
	}
	ref := auth.SessionRef(rc.Ctx.URL.Value, username)
	return &auth.Resolver{
		URL:      rc.Ctx.URL.Value,
		Username: username,
		Env:      a.Env,
		Store:    auth.Store{Dir: rc.Paths.StateDir},
		Secret:   configuredSecret(a.Env, host, rc.Paths, ref),
		HTTP:     a.httpClient(),
		Now:      time.Now,
		Warn:     a.warn,
	}, nil
}

func (a *App) resolverTokenSource(_ context.Context, rc *RunContext) (taiga.TokenSource, error) {
	r, err := a.resolver(rc)
	if err != nil {
		return nil, err
	}
	return r, nil
}
