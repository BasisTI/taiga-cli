package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/auth"
	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *App) authCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Log in, inspect and refresh Taiga credentials"}
	cmd.AddCommand(a.authLoginCmd(), a.authRefreshCmd(), a.authStatusCmd(), a.authLogoutCmd())
	return cmd
}

func (a *App) stdinIsTTY() bool {
	f, ok := a.In.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *App) authLoginCmd() *cobra.Command {
	var username, secretCmd string
	var passwordStdin, insecure bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store the secret (keyring, secret command or --insecure-storage)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if username == "" {
				return &output.Error{Code: "usage", Cause: "--username is required", Exit: output.ExitUsage}
			}
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			if _, known := rc.File.Host(rc.Ctx.URL.Value); !known && strings.HasPrefix(rc.Ctx.URL.Source, "file:") {
				// A cloned repository must not choose where the typed password goes.
				return &output.Error{Code: "auth_untrusted_url", Source: "file", Stage: strings.TrimPrefix(rc.Ctx.URL.Source, "file:"), Cause: "refusing to send the password to " + rc.Ctx.URL.Value + ": the URL comes only from .taiga.toml", Recovery: "run `taiga auth login --url " + rc.Ctx.URL.Value + "` if you trust it", Exit: output.ExitAuth}
			}
			var pw []byte
			switch {
			case secretCmd != "":
				if pw, err = (auth.CommandSecret{Args: strings.Fields(secretCmd)}).Password(ctx); err != nil {
					return err
				}
			case passwordStdin:
				b, err := io.ReadAll(io.LimitReader(a.In, 64<<10))
				if err != nil {
					return err
				}
				pw = bytes.TrimRight(b, "\r\n")
			case a.stdinIsTTY():
				_, _ = io.WriteString(a.Err, "Password: ")
				b, err := term.ReadPassword(int(a.In.(*os.File).Fd()))
				_, _ = io.WriteString(a.Err, "\n")
				if err != nil {
					return err
				}
				pw = b
			default:
				return &output.Error{Code: "usage", Cause: "no password source and stdin is not a terminal", Recovery: "use --password-stdin or --secret-command", Exit: output.ExitUsage}
			}
			host, _ := rc.File.Host(rc.Ctx.URL.Value)
			host.URL, host.Username = rc.Ctx.URL.Value, username
			if a.flagProject != "" {
				host.Project = a.flagProject
			}
			r, err := a.resolver(rc)
			if err != nil {
				return err
			}
			r.Username = username
			ref := auth.SessionRef(host.URL, username)
			if _, err := r.LoginWith(ctx, pw); err != nil {
				return err
			}
			switch {
			case secretCmd != "":
				host.SecretSource, host.SecretCommand = "secret_command", strings.Fields(secretCmd)
			case insecure:
				if err := (auth.FileSecret{Path: filepath.Join(rc.Paths.SecretsDir, ref)}).Put(pw); err != nil {
					return err
				}
				host.SecretSource, host.SecretCommand = "file", nil
			default:
				if err := (auth.Keyring{Ref: ref}).Put(ctx, pw); err != nil {
					_ = r.Store.Delete(ref)
					e := output.AsError(err)
					e.Recovery = "see README \"Headless Linux keyring\", or re-run with --secret-command or --insecure-storage"
					return e
				}
				host.SecretSource, host.SecretCommand = "keyring", nil
			}
			rc.File.Upsert(host)
			if rc.File.DefaultHost == "" {
				rc.File.DefaultHost = host.URL
			}
			if err := config.Save(rc.Paths.ConfigFile, rc.File); err != nil {
				return err
			}
			a.Env = overlayEnv(a.Env, map[string]string{"TAIGA_USERNAME": username})
			return a.printStatus(ctx, false)
		},
	}
	f := cmd.Flags()
	f.StringVar(&username, "username", "", "Taiga username (required)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	f.StringVar(&secretCmd, "secret-command", "", "command that prints the password (split on spaces, no shell)")
	f.BoolVar(&insecure, "insecure-storage", false, "store the password in a 0600 file instead of the keyring")
	return cmd
}

func overlayEnv(base func(string) string, over map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := over[k]; ok {
			return v
		}
		return base(k)
	}
}

func (a *App) authRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Renew the stored session now (run outside sandboxes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			r, err := a.resolver(rc)
			if err != nil {
				return err
			}
			if _, err := r.ForceRefresh(cmd.Context()); err != nil {
				return err
			}
			return a.printStatus(cmd.Context(), false)
		},
	}
}

func (a *App) authStatusCmd() *cobra.Command {
	var diagnose bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show identity, URL/project provenance and session state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.printStatus(cmd.Context(), diagnose)
		},
	}
	cmd.Flags().BoolVar(&diagnose, "diagnose", false, "test every credential source and the environment")
	return cmd
}

type statusView struct {
	URL     config.Value  `json:"url"`
	Project config.Value  `json:"project"`
	User    *userView     `json:"user,omitempty"`
	Session sessionView   `json:"session"`
	Checks  []auth.Check  `json:"checks,omitempty"`
	Error   *output.Error `json:"error,omitempty"`
}
type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
type sessionView struct {
	Path      string `json:"path"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Writable  bool   `json:"writable"`
}

func (a *App) printStatus(ctx context.Context, diagnose bool) error {
	rc, err := a.runContext()
	if err != nil {
		return err
	}
	r, err := a.resolver(rc)
	if err != nil {
		return err
	}
	ref := auth.SessionRef(r.URL, r.Username)
	v := statusView{URL: rc.Ctx.URL, Project: rc.Ctx.Project, Session: sessionView{Path: r.Store.Path(ref), Writable: r.Store.Writable()}}
	var identityErr *output.Error
	c := taiga.New(r.URL, r, taiga.WithHTTPClient(a.httpClient()))
	resp, err := c.Do(ctx, taiga.Request{Method: http.MethodGet, Path: "users/me"})
	if err != nil {
		identityErr = taiga.ToOutput(err)
		v.Error = identityErr
	} else {
		var u userView
		_ = json.Unmarshal(resp.Body, &u)
		v.User = &u
	}
	if s, err := r.Store.Load(ref); err == nil {
		v.Session.ExpiresAt = s.Expiry.Format(time.RFC3339)
	}
	if diagnose {
		var probe func(context.Context) error
		host, _ := rc.File.Host(r.URL)
		if host.SecretSource == "keyring" || host.SecretSource == "" {
			probe = auth.Keyring{Ref: ref}.Available
		}
		project := &auth.ProjectInput{Selected: rc.Ctx.Project.Value, Source: rc.Ctx.Project.Source, AuthFailed: identityErr != nil,
			Load: func(ctx context.Context) (map[string]any, error) {
				s, err := app.New(ctx, c, rc.Ctx.Project.Value)
				if err != nil {
					return nil, err
				}
				return s.Project, nil
			}}
		v.Checks = auth.Diagnose(ctx, auth.DiagnoseInput{Env: a.Env, Store: r.Store, Ref: ref, Secret: r.Secret, KeyringProbe: probe, StdinTTY: a.stdinIsTTY(), Now: time.Now, Project: project})
	}
	mode, _ := output.DetectMode(a.output, a.OutTTY)
	if mode == output.JSON {
		if err := output.WriteJSON(a.Out, v); err != nil {
			return err
		}
	} else {
		fields := []output.Field{{Key: "url", Value: v.URL.Value + " (" + v.URL.Source + ")"}, {Key: "project", Value: v.Project.Value + " (" + v.Project.Source + ")"}}
		if v.User != nil {
			fields = append(fields, output.Field{Key: "user", Value: v.User.Username})
		}
		fields = append(fields, output.Field{Key: "session", Value: v.Session.ExpiresAt + " " + v.Session.Path})
		for _, ch := range v.Checks {
			fields = append(fields, output.Field{Key: "check " + ch.Name, Value: ch.Status + " " + ch.Detail})
		}
		for i, f := range fields {
			if strings.ContainsFunc(f.Value, unsafeRune) {
				fields[i].Value = strconv.Quote(f.Value) // one line per key: the server's text cannot forge another check
			}
		}
		if err := output.WriteFields(a.Out, fields); err != nil {
			return err
		}
	}
	if identityErr != nil {
		identityErr.Recovery = "run `taiga auth status --diagnose`; " + identityErr.Recovery
		return identityErr
	}
	return nil
}

func (a *App) authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the local session and stored secret",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			r, err := a.resolver(rc)
			if err != nil {
				return err
			}
			ref := auth.SessionRef(r.URL, r.Username)
			if err := r.Store.Delete(ref); err != nil {
				return err
			}
			host, _ := rc.File.Host(r.URL)
			switch host.SecretSource {
			case "keyring":
				// The session is gone already; a secret that could not be deleted must not look like success.
				if err := (auth.Keyring{Ref: ref}).Delete(cmd.Context()); err != nil && output.AsError(err).Code != "secret_missing" {
					return err
				}
			case "file":
				if err := os.Remove(filepath.Join(rc.Paths.SecretsDir, ref)); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			_, err = io.WriteString(a.Out, "logged out of "+r.URL+" ("+r.Username+")\n")
			return err
		},
	}
}
