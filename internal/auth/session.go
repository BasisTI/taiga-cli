// Package auth resolves Taiga credentials: env, session cache, refresh and secret sources.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/taiga-cli/internal/config"
	"golang.org/x/sys/unix"
)

var ErrReadOnly = errors.New("session store is read-only")

type Session struct {
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	AuthToken string    `json:"auth_token"`
	Refresh   string    `json:"refresh"`
	UserID    int64     `json:"user_id"`
	Expiry    time.Time `json:"exp"`
}

func SessionRef(url, username string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimRight(url, "/")+"\x00"+username)))
}

type Store struct{ Dir string }

func (s Store) Path(ref string) string { return filepath.Join(s.Dir, "sessions", ref+".json") }

func (s Store) Load(ref string) (Session, error) {
	var sess Session
	b, err := os.ReadFile(s.Path(ref))
	if err != nil {
		return sess, err
	}
	if err := json.Unmarshal(b, &sess); err != nil {
		return sess, fmt.Errorf("session cache %s is corrupt: %w", s.Path(ref), err)
	}
	return sess, nil
}

func isReadOnly(err error) bool {
	return errors.Is(err, syscall.EROFS) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EPERM)
}

func (s Store) Save(ref string, sess Session) error {
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.Path(ref), b); err != nil {
		if isReadOnly(err) {
			return fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return err
	}
	return nil
}

func (s Store) Delete(ref string) error {
	err := os.Remove(s.Path(ref))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Writable reports whether the sessions directory accepts new files.
func (s Store) Writable() bool {
	dir := filepath.Join(s.Dir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}

// Lock takes an exclusive flock so concurrent processes do not refresh the same session twice.
func (s Store) Lock(ctx context.Context, ref string) (func(), error) {
	dir := filepath.Join(s.Dir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		if isReadOnly(err) {
			return nil, fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ref+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if isReadOnly(err) {
			return nil, fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("timed out waiting for session lock: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// JWTExpiry reads the exp claim without verifying the signature (the server verifies it).
func JWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, errors.New("token has no exp claim")
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}
