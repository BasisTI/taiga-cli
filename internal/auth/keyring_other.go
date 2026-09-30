//go:build !linux

// Adapted from sgo-cli (Basis Tecnologia da Informação, Apache-2.0).

package auth

import "context"

type Keyring struct{ Ref string }

func unsupported() error {
	return authErr("keyring_unsupported", "keyring", "Secret Service is only supported on Linux", "use --secret-command")
}

func (Keyring) Name() string                             { return "keyring" }
func (Keyring) Password(context.Context) ([]byte, error) { return nil, unsupported() }
func (Keyring) Put(context.Context, []byte) error        { return unsupported() }
func (Keyring) Delete(context.Context) error             { return unsupported() }
func (Keyring) Available(context.Context) error          { return unsupported() }
