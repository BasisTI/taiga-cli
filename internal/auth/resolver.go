package auth

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// refreshInvalidatesPrevious reflects TestProbeRefreshRotationInvalidatesPrevious (docs/api-notes.md).
// When true, a read-only session store must not refresh: the rotated token could not be persisted.
const refreshInvalidatesPrevious = true

const expiryMargin = 60 * time.Second

type Resolver struct {
	URL, Username string
	Env           func(string) string
	Store         Store
	Secret        SecretSource
	HTTP          *http.Client
	Now           func() time.Time
	Warn          func(code, message string)
}

func (r *Resolver) ref() string { return SessionRef(r.URL, r.Username) }

func (r *Resolver) valid(s Session) bool {
	return s.AuthToken != "" && r.Now().Add(expiryMargin).Before(s.Expiry)
}

func bearer(s Session) taiga.Token { return taiga.Token{Type: "Bearer", Value: s.AuthToken} }

func (r *Resolver) Token(ctx context.Context) (taiga.Token, error) {
	if v := r.Env("TAIGA_TOKEN"); v != "" {
		typ := r.Env("TAIGA_TOKEN_TYPE")
		if typ == "" {
			typ = "Bearer"
		}
		return taiga.Token{Type: typ, Value: v}, nil
	}
	if r.Username == "" {
		return taiga.Token{}, authErr("auth_no_source", "config", "no username configured for "+r.URL, "run `taiga auth login` or set TAIGA_TOKEN")
	}
	sess, loadErr := r.Store.Load(r.ref())
	if loadErr == nil && r.valid(sess) {
		return bearer(sess), nil
	}
	unlock, lockErr := r.Store.Lock(ctx, r.ref())
	// An existing lock file can be opened in a read-only directory, so probe the directory too:
	// refreshing without being able to save would burn the rotated refresh token.
	readOnly := errors.Is(lockErr, ErrReadOnly) || (lockErr == nil && !r.Store.Writable())
	if lockErr != nil && !readOnly {
		return taiga.Token{}, lockErr
	}
	if unlock != nil {
		defer unlock()
		if s2, err := r.Store.Load(r.ref()); err == nil {
			sess, loadErr = s2, nil
			if r.valid(sess) {
				return bearer(sess), nil
			}
		}
	}
	hadSession := loadErr == nil
	if hadSession && sess.Refresh != "" && (!readOnly || !refreshInvalidatesPrevious) {
		if s, err := r.refresh(ctx, sess); err == nil {
			return bearer(s), nil
		} else if output.AsError(err).Code != "session_expired" {
			return taiga.Token{}, err
		}
	}
	secret := r.Secret
	if hadSession && readOnly {
		// In a sandbox only an env password may log in again; the stored secret stays for `taiga auth refresh`.
		secret = envOnly(secret)
	}
	if secret == nil {
		if hadSession && readOnly {
			return taiga.Token{}, &output.Error{Code: "session_expired", Source: "session_cache", Stage: r.Store.Path(r.ref()), Cause: "session expired and the session cache is read-only", Recovery: "run `taiga auth refresh` outside the sandbox", Exit: output.ExitAuth}
		}
		if hadSession {
			return taiga.Token{}, authErr("session_expired", "session_cache", "session expired and no secret source is configured", "run `taiga auth login`")
		}
		return taiga.Token{}, authErr("auth_no_source", "config", "no session and no secret source for "+r.Username, "run `taiga auth login`")
	}
	pw, err := secret.Password(ctx)
	if err != nil {
		if hadSession && readOnly {
			e := output.AsError(err)
			e.Recovery = "run `taiga auth refresh` outside the sandbox (" + e.Recovery + ")"
			return taiga.Token{}, e
		}
		return taiga.Token{}, err
	}
	s, err := r.LoginWith(ctx, pw)
	if err != nil {
		return taiga.Token{}, err
	}
	return bearer(s), nil
}

func (r *Resolver) persist(s Session) {
	if err := r.Store.Save(r.ref(), s); err != nil && r.Warn != nil {
		if errors.Is(err, ErrReadOnly) {
			r.Warn("session_cache_readonly", "session cache is read-only; token kept in memory for this run")
		} else {
			r.Warn("session_cache_write_failed", err.Error())
		}
	}
}

func (r *Resolver) refresh(ctx context.Context, sess Session) (Session, error) {
	tok, rt, err := RefreshToken(ctx, r.HTTP, r.URL, sess.Refresh)
	if err != nil {
		return Session{}, err
	}
	sess.AuthToken, sess.Refresh = tok, rt
	if exp, err := JWTExpiry(tok); err == nil {
		sess.Expiry = exp
	} else {
		sess.Expiry = r.Now().Add(time.Hour)
	}
	r.persist(sess)
	return sess, nil
}

// LoginWith authenticates with a password and stores the new session.
func (r *Resolver) LoginWith(ctx context.Context, password []byte) (Session, error) {
	res, err := Login(ctx, r.HTTP, r.URL, r.Username, password)
	if err != nil {
		return Session{}, err
	}
	s := Session{URL: r.URL, Username: r.Username, AuthToken: res.AuthToken, Refresh: res.Refresh, UserID: res.UserID}
	if exp, err := JWTExpiry(res.AuthToken); err == nil {
		s.Expiry = exp
	} else {
		s.Expiry = r.Now().Add(time.Hour)
	}
	r.persist(s)
	return s, nil
}

// ForceRefresh renews the stored session regardless of its expiry (taiga auth refresh).
func (r *Resolver) ForceRefresh(ctx context.Context) (Session, error) {
	readOnly := &output.Error{Code: "session_cache_readonly", Source: "session_cache", Stage: r.Store.Path(r.ref()), Cause: "cannot renew: the session cache is read-only", Recovery: "run `taiga auth refresh` outside the sandbox", Exit: output.ExitAuth}
	unlock, err := r.Store.Lock(ctx, r.ref())
	if errors.Is(err, ErrReadOnly) {
		return Session{}, readOnly
	}
	if err != nil {
		return Session{}, err
	}
	defer unlock()
	// An existing lock file opens even in a read-only directory: probe before spending the refresh token.
	if !r.Store.Writable() {
		return Session{}, readOnly
	}
	sess, err := r.Store.Load(r.ref())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Session{}, err
	}
	if err == nil && sess.Refresh != "" {
		s, rerr := r.refresh(ctx, sess)
		if rerr == nil {
			return s, nil
		}
		// Only an expired refresh falls back to a password login; network or server errors are reported.
		if output.AsError(rerr).Code != "session_expired" {
			return Session{}, rerr
		}
	}
	if r.Secret == nil {
		return Session{}, authErr("session_expired", "session_cache", "no valid session and no secret source", "run `taiga auth login`")
	}
	pw, err := r.Secret.Password(ctx)
	if err != nil {
		return Session{}, err
	}
	return r.LoginWith(ctx, pw)
}

// envOnly returns the env password source within s, or nil.
func envOnly(s SecretSource) SecretSource {
	switch v := s.(type) {
	case EnvPassword:
		return v
	case FirstOf:
		for _, src := range v {
			if e, ok := src.(EnvPassword); ok {
				return e
			}
		}
	}
	return nil
}
