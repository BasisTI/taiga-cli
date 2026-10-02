package auth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Data is the structured form of Detail, for checks that have one (project).
	Data map[string]any `json:"data,omitempty"`
}

type DiagnoseInput struct {
	Env          func(string) string
	Store        Store
	Ref          string
	Secret       SecretSource
	KeyringProbe func(context.Context) error
	StdinTTY     bool
	Now          func() time.Time
	// Project, when set, adds the project check at the end.
	Project *ProjectInput
}

// ProjectInput is the selected project and how to read it. Load reads the project object as
// Taiga answers GET projects/<id> or projects/by_slug; it is not called when the check is skipped.
type ProjectInput struct {
	Selected   string
	Source     string
	AuthFailed bool
	Load       func(context.Context) (map[string]any, error)
}

// projectPermissions are the permissions the curated commands need, in the order reported.
var projectPermissions = []string{"view_us", "modify_us", "add_us", "comment_us", "view_tasks", "add_task", "modify_task", "modify_epic", "admin_project_values"}

func Diagnose(ctx context.Context, in DiagnoseInput) []Check {
	var out []Check
	add := func(name, status, detail string) { out = append(out, Check{Name: name, Status: status, Detail: detail}) }
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
		add("keyring", "failed", e.Code+": "+e.Cause+"; see README section \"Headless Linux keyring\"")
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
	if in.Project != nil {
		out = append(out, checkProject(ctx, *in.Project))
	}
	return out
}

// checkProject reads the selected project and reports membership, the permissions the curated
// commands need, the modules and the swimlanes. It only reads. Missing permissions are listed
// but do not fail the check: they limit some commands, not the access to the project.
func checkProject(ctx context.Context, in ProjectInput) Check {
	c := Check{Name: "project"}
	switch {
	case in.Selected == "":
		c.Status, c.Detail = "skipped", "no project selected"
		return c
	case in.AuthFailed:
		c.Status, c.Detail = "skipped", "identity check (users/me) failed"
		return c
	}
	p, err := in.Load(ctx)
	if err != nil {
		e := output.AsError(err)
		recovery := e.Recovery
		switch e.Code {
		case "not_found":
			recovery = "the project does not exist or this account cannot see it; check the slug with `taiga project list`"
		case "forbidden":
			recovery = "this account cannot see the project; ask a project admin to add it as a member"
		}
		c.Status, c.Detail = "failed", in.Selected+" ("+in.Source+"): "+e.Code+": "+e.Cause
		if recovery != "" {
			c.Detail += "; " + recovery
		}
		return c
	}
	have := map[string]bool{}
	perms, _ := p["my_permissions"].([]any)
	for _, x := range perms {
		if s, ok := x.(string); ok {
			have[s] = true
		}
	}
	missing := []string{}
	for _, perm := range projectPermissions {
		if !have[perm] {
			missing = append(missing, perm)
		}
	}
	lanes, _ := p["swimlanes"].([]any)
	onOff := func(k string) string {
		if p[k] == true {
			return "on"
		}
		return "off"
	}
	c.Status = "ok"
	c.Detail = fmt.Sprintf("id=%v slug=%v source=%s member=%v admin=%v; modules: epics=%s kanban=%s backlog=%s; swimlanes=%d",
		p["id"], p["slug"], in.Source, p["i_am_member"] == true, p["i_am_admin"] == true,
		onOff("is_epics_activated"), onOff("is_kanban_activated"), onOff("is_backlog_activated"), len(lanes))
	if len(missing) > 0 {
		c.Detail += "; missing permissions: " + strings.Join(missing, ", ")
	}
	c.Data = map[string]any{"id": p["id"], "slug": p["slug"], "source": in.Source, "is_member": p["i_am_member"] == true, "is_admin": p["i_am_admin"] == true,
		"missing_permissions": missing, "is_epics_activated": p["is_epics_activated"] == true, "is_kanban_activated": p["is_kanban_activated"] == true,
		"is_backlog_activated": p["is_backlog_activated"] == true, "swimlanes": len(lanes)}
	return c
}
