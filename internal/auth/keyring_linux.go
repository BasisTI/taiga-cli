//go:build linux

// Adapted from sgo-cli (Basis Tecnologia da Informação, Apache-2.0).

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Keyring stores the password in the Secret Service under the given reference.
// It never invokes Unlock or Prompt: an agent must not wait for desktop interaction.
type Keyring struct{ Ref string }

const keyringRecovery = "see README section \"Headless Linux keyring\"; or use --secret-command"

const service = "org.freedesktop.secrets"
const root = dbus.ObjectPath("/org/freedesktop/secrets")
const iface = "org.freedesktop.Secret."

type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// keyringTimeout bounds every Secret Service operation.
var keyringTimeout = 5 * time.Second

// connectCause reports a connection that ran out of time as the deadline itself:
// godbus returns its own error when the bus accepts but never completes the handshake.
func connectCause(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func storeError(stage string, cause error) error {
	code := "keyring_unavailable"
	message := "cannot reach the Secret Service"
	recovery := keyringRecovery
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled) {
		code = "keyring_timeout"
		message = "the Secret Service did not answer in time"
	}
	var de dbus.Error
	var dep *dbus.Error
	if errors.As(cause, &dep) && dep != nil {
		de = *dep
	} else {
		errors.As(cause, &de)
	}
	name := de.Name
	if strings.HasPrefix(name, "org.freedesktop.DBus.Error.Spawn.") {
		// Bus activation of the Secret Service failed: same as having none.
		name = "org.freedesktop.DBus.Error.ServiceUnknown"
	}
	switch name {
	case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner":
		code = "keyring_service_unavailable"
		message = "no Secret Service (org.freedesktop.secrets) on this session bus"
		recovery = keyringRecovery
	case "org.freedesktop.Secret.Error.IsLocked":
		return lockedError()
	case "org.freedesktop.DBus.Error.AccessDenied":
		code = "keyring_access_denied"
		message = "the Secret Service denied access"
	case "org.freedesktop.DBus.Error.NoReply", "org.freedesktop.DBus.Error.Timeout":
		code = "keyring_timeout"
		message = "the Secret Service did not answer in time"
	}
	return authErr(code, "keyring", fmt.Sprintf("%s (stage: %s)", message, stage), recovery)
}
func lockedError() error {
	return authErr("keyring_locked", "keyring", "the Secret Service collection is locked; unlock it (gnome-keyring-daemon --unlock)", keyringRecovery)
}
func promptError() error {
	return authErr("keyring_prompt_required", "keyring", "the keyring requires an interactive prompt; taiga never opens one", keyringRecovery)
}

func (Keyring) Name() string { return "keyring" }

func (k Keyring) Password(ctx context.Context) ([]byte, error) {
	return k.operation(ctx, "get", nil)
}
func (k Keyring) Put(ctx context.Context, value []byte) error {
	_, e := k.operation(ctx, "put", value)
	return e
}
func (k Keyring) Delete(ctx context.Context) error {
	_, e := k.operation(ctx, "delete", nil)
	return e
}

// Available reports whether a Secret Service owns its name on the session bus.
func (Keyring) Available(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, keyringTimeout)
	defer cancel()
	conn, e := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if e != nil {
		return storeError("connect", connectCause(ctx, e))
	}
	defer func() { _ = conn.Close() }()
	var owned bool
	if e = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, service).Store(&owned); e != nil {
		return storeError("name_has_owner", e)
	}
	if !owned {
		return authErr("keyring_service_unavailable", "keyring", "no Secret Service (org.freedesktop.secrets) on this session bus", keyringRecovery)
	}
	return nil
}

func (k Keyring) operation(parent context.Context, op string, value []byte) ([]byte, error) {
	ref := k.Ref
	if ref == "" {
		return nil, authErr("secret_missing", "keyring", "missing credential reference", keyringRecovery)
	}
	ctx, cancel := context.WithTimeout(parent, keyringTimeout)
	defer cancel()
	conn, e := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if e != nil {
		return nil, storeError("connect", connectCause(ctx, e))
	}
	defer func() { _ = conn.Close() }()
	obj := conn.Object(service, root)
	attributes := map[string]string{"service": "taiga-cli", "account": ref}
	var unlocked, locked []dbus.ObjectPath
	if e = obj.CallWithContext(ctx, iface+"Service.SearchItems", 0, attributes).Store(&unlocked, &locked); e != nil {
		return nil, storeError("search_items", e)
	}
	if len(locked) > 0 {
		return nil, lockedError()
	}
	if len(unlocked) > 1 {
		return nil, authErr("secret_ambiguous", "keyring", "more than one credential for this reference", keyringRecovery)
	}
	if op != "put" && len(unlocked) == 0 {
		return nil, authErr("secret_missing", "keyring", "no credential stored in the keyring", keyringRecovery)
	}
	if op == "delete" {
		var prompt dbus.ObjectPath
		if e = conn.Object(service, unlocked[0]).CallWithContext(ctx, iface+"Item.Delete", 0).Store(&prompt); e != nil {
			return nil, storeError("delete_item", e)
		}
		if prompt != "/" {
			return nil, promptError()
		}
		return nil, nil
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	if e = obj.CallWithContext(ctx, iface+"Service.OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); e != nil {
		return nil, storeError("open_session", e)
	}
	defer conn.Object(service, session).CallWithContext(ctx, iface+"Session.Close", 0)
	if op == "get" {
		var s secret
		if e = conn.Object(service, unlocked[0]).CallWithContext(ctx, iface+"Item.GetSecret", 0, session).Store(&s); e != nil {
			return nil, storeError("get_secret", e)
		}
		if len(s.Value) == 0 {
			return nil, authErr("secret_missing", "keyring", "no credential stored in the keyring", keyringRecovery)
		}
		return s.Value, nil
	}
	var collection dbus.ObjectPath
	if e = obj.CallWithContext(ctx, iface+"Service.ReadAlias", 0, "default").Store(&collection); e != nil {
		return nil, storeError("read_default_alias", e)
	}
	if collection == "/" {
		return nil, authErr("keyring_no_default", "keyring", "the Secret Service has no default collection", keyringRecovery)
	}
	var isLocked dbus.Variant
	if e = conn.Object(service, collection).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, iface+"Collection", "Locked").Store(&isLocked); e != nil {
		return nil, storeError("collection_locked", e)
	}
	lockedValue, ok := isLocked.Value().(bool)
	if !ok {
		return nil, storeError("collection_locked_response", nil)
	}
	if lockedValue {
		return nil, lockedError()
	}
	properties := map[string]dbus.Variant{iface + "Item.Label": dbus.MakeVariant("taiga-cli"), iface + "Item.Attributes": dbus.MakeVariant(attributes)}
	var item, prompt dbus.ObjectPath
	s := secret{Session: session, Parameters: []byte{}, Value: value, ContentType: "text/plain; charset=utf8"}
	if e = conn.Object(service, collection).CallWithContext(ctx, iface+"Collection.CreateItem", 0, properties, s, true).Store(&item, &prompt); e != nil {
		return nil, storeError("create_item", e)
	}
	if prompt != "/" {
		return nil, promptError()
	}
	return nil, nil
}
