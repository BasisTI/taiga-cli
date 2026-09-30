//go:build linux

// Adapted from sgo-cli (Basis Tecnologia da Informação, Apache-2.0).

package auth

import (
	"bufio"
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/godbus/dbus/v5"
)

// A separate bus and fake service exercise the real provider's D-Bus wire contract.
type fakeSecrets struct {
	mu     sync.Mutex
	value  []byte
	locked bool
	exists bool
}

func (f *fakeSecrets) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	free, locked := []dbus.ObjectPath{}, []dbus.ObjectPath{}
	if f.exists {
		if f.locked {
			locked = append(locked, "/item")
		} else {
			free = append(free, "/item")
		}
	}
	return free, locked, nil
}
func (f *fakeSecrets) OpenSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	return dbus.MakeVariant(""), "/session", nil
}
func (f *fakeSecrets) ReadAlias(alias string) (dbus.ObjectPath, *dbus.Error) {
	return "/collection", nil
}
func (f *fakeSecrets) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return dbus.MakeVariant(f.locked), nil
}
func (f *fakeSecrets) CreateItem(props map[string]dbus.Variant, s secret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = append([]byte{}, s.Value...)
	f.exists = true
	return "/item", "/", nil
}
func (f *fakeSecrets) GetSecret(session dbus.ObjectPath) (secret, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return secret{Session: session, Parameters: []byte{}, Value: append([]byte{}, f.value...), ContentType: "text/plain; charset=utf8"}, nil
}
func (f *fakeSecrets) Delete() (dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = false
	f.value = nil
	return "/", nil
}
func (f *fakeSecrets) Close() *dbus.Error { return nil }
func TestKeyringRoundTripOnIsolatedBus(t *testing.T) {
	bin, e := exec.LookPath("dbus-daemon")
	if e != nil {
		t.Skip("dbus-daemon is not installed")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1")
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("bus printed no address")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", scanner.Text())
	conn, e := dbus.ConnectSessionBus()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = conn.Close() }()
	if _, e = conn.RequestName(service, dbus.NameFlagDoNotQueue); e != nil {
		t.Fatal(e)
	}
	f := &fakeSecrets{}
	for _, binding := range []struct {
		path dbus.ObjectPath
		name string
	}{{root, iface + "Service"}, {"/collection", iface + "Collection"}, {"/collection", "org.freedesktop.DBus.Properties"}, {"/item", iface + "Item"}, {"/session", iface + "Session"}} {
		if e = conn.Export(f, binding.path, binding.name); e != nil {
			t.Fatal(e)
		}
	}
	k := Keyring{Ref: "test-ref"}
	ctx := context.Background()
	if e = k.Put(ctx, []byte("not-a-real-password")); e != nil {
		t.Fatal(e)
	}
	b, e := k.Password(ctx)
	if e != nil || string(b) != "not-a-real-password" {
		t.Fatalf("round trip failed: %v", e)
	}
	f.mu.Lock()
	f.locked = true
	f.mu.Unlock()
	if _, e = k.Password(ctx); e == nil {
		t.Fatal("a locked keyring returned the secret")
	}
	f.mu.Lock()
	f.locked = false
	f.mu.Unlock()
	if e = k.Delete(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = k.Password(ctx); e == nil {
		t.Fatal("secret was not deleted")
	}
}

func TestKeyringReportsMissingDefaultCollection(t *testing.T) {
	conn := isolatedBus(t)
	if _, err := conn.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	f := &noDefaultSecrets{fakeSecrets: fakeSecrets{}}
	if err := conn.Export(f, root, iface+"Service"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(&f.fakeSecrets, "/session", iface+"Session"); err != nil {
		t.Fatal(err)
	}
	err := (Keyring{Ref: "test-ref"}).Put(context.Background(), []byte("fake"))
	if err == nil || output.AsError(err).Code != "keyring_no_default" {
		t.Fatalf("hidden cause: %v", err)
	}
}

type noDefaultSecrets struct{ fakeSecrets }

func (f *noDefaultSecrets) ReadAlias(string) (dbus.ObjectPath, *dbus.Error) { return "/", nil }
func TestKeyringReportsMissingSecretService(t *testing.T) {
	isolatedBus(t)
	_, err := (Keyring{Ref: "test-ref"}).Password(context.Background())
	if err == nil || output.AsError(err).Code != "keyring_service_unavailable" {
		t.Fatalf("hidden cause: %v", err)
	}
}
func isolatedBus(t *testing.T) *dbus.Conn {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scan := bufio.NewScanner(stdout)
	if !scan.Scan() {
		t.Fatal("bus printed no address")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", scan.Text())
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestKeyringAvailable(t *testing.T) {
	conn := isolatedBus(t)
	k := Keyring{Ref: "test-ref"}
	if err := k.Available(context.Background()); output.AsError(err).Code != "keyring_service_unavailable" {
		t.Fatalf("without owner: %v", err)
	}
	if _, err := conn.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if err := k.Available(context.Background()); err != nil {
		t.Fatalf("with owner: %v", err)
	}
}

func TestKeyringErrorsAreAuthErrorsWithRecovery(t *testing.T) {
	isolatedBus(t)
	e := output.AsError((Keyring{Ref: "test-ref"}).Put(context.Background(), []byte("fake")))
	if e.Exit != output.ExitAuth || e.Source != "keyring" || e.Recovery != keyringRecovery {
		t.Fatalf("%+v", e)
	}
}

// Codex review #5: a bus that accepts the connection and never answers is a timeout.
func TestKeyringStalledBusIsTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bus")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		if c, err := ln.Accept(); err == nil {
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+path)
	old := keyringTimeout
	keyringTimeout = 300 * time.Millisecond
	defer func() { keyringTimeout = old }()
	start := time.Now()
	k := Keyring{Ref: "r"}
	_, err = k.Password(context.Background())
	if e := output.AsError(err); e.Code != "keyring_timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("%+v after %s", e, time.Since(start))
	}
	if e := output.AsError(k.Available(context.Background())); e.Code != "keyring_timeout" {
		t.Fatalf("available: %+v", e)
	}
}
