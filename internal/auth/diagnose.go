package auth

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DiagnoseInput struct {
	Env          func(string) string
	Store        Store
	Ref          string
	Secret       SecretSource
	KeyringProbe func(context.Context) error
	StdinTTY     bool
	Now          func() time.Time
}

func Diagnose(ctx context.Context, in DiagnoseInput) []Check {
	var out []Check
	add := func(name, status, detail string) { out = append(out, Check{name, status, detail}) }
	if in.Env("TAIGA_TOKEN") != "" {
		add("env_token", "ok", "TAIGA_TOKEN is set and takes precedence")
	} else {
		add("env_token", "skipped", "")
	}
	if (EnvPassword{Env: in.Env}).Configured() {
		add("env_password", "ok", "")
	} else {
		add("env_password", "skipped", "")
	}
	if in.Secret == nil {
		add("secret_source", "skipped", "none configured")
	} else if _, err := in.Secret.Password(ctx); err != nil {
		e := output.AsError(err)
		add("secret_source", "failed", in.Secret.Name()+": "+e.Code+": "+e.Cause)
	} else {
		add("secret_source", "ok", in.Secret.Name())
	}
	if in.KeyringProbe == nil {
		add("keyring", "skipped", "")
	} else if err := in.KeyringProbe(ctx); err != nil {
		e := output.AsError(err)
		add("keyring", "failed", e.Code+": "+e.Cause)
	} else {
		add("keyring", "ok", "org.freedesktop.secrets is available")
	}
	sess, err := in.Store.Load(in.Ref)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		add("session_cache", "failed", "missing: "+in.Store.Path(in.Ref))
	case err != nil:
		add("session_cache", "failed", err.Error())
	case in.Now().After(sess.Expiry):
		add("session_cache", "failed", "expired at "+sess.Expiry.Format(time.RFC3339))
	default:
		add("session_cache", "ok", "valid until "+sess.Expiry.Format(time.RFC3339))
	}
	if in.Store.Writable() {
		add("session_cache_writable", "ok", in.Store.Dir)
	} else {
		add("session_cache_writable", "failed", "read-only (sandbox?): "+in.Store.Dir)
	}
	if in.Env("CODEX_SANDBOX_NETWORK_DISABLED") != "" {
		add("sandbox", "failed", "CODEX_SANDBOX_NETWORK_DISABLED=1: unix sockets and network are blocked")
	} else {
		add("sandbox", "ok", "")
	}
	if a := in.Env("DBUS_SESSION_BUS_ADDRESS"); a == "" {
		add("dbus", "skipped", "DBUS_SESSION_BUS_ADDRESS is empty")
	} else {
		add("dbus", "ok", a)
	}
	if in.StdinTTY {
		add("tty", "ok", "")
	} else {
		add("tty", "skipped", "no TTY: pinentry prompts cannot work")
	}
	return out
}
