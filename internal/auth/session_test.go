package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + itoa64(exp.Unix()) + `}`))
	return "eyJhbGciOiJIUzI1NiJ9." + payload + ".sig"
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Unix(1790000000, 0)
	got, err := JWTExpiry(fakeJWT(exp))
	if err != nil || !got.Equal(exp) {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := JWTExpiry("not-a-jwt"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionRefIsStable(t *testing.T) {
	a, again := SessionRef("https://a", "u"), SessionRef("https://a", "u")
	if a != again || a == SessionRef("https://a", "v") || a == SessionRef("https://b", "u") {
		t.Fatal("ref must depend on url and user only")
	}
}

func TestStoreSaveLoadPermissions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ref := SessionRef("https://a", "u")
	if _, err := s.Load(ref); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	in := Session{URL: "https://a", Username: "u", AuthToken: "t", Refresh: "r", UserID: 7, Expiry: time.Unix(1790000000, 0).UTC()}
	if err := s.Save(ref, in); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(s.Path(ref))
	dinfo, _ := os.Stat(filepath.Dir(s.Path(ref)))
	if info.Mode().Perm() != 0o600 || dinfo.Mode().Perm() != 0o700 {
		t.Fatalf("perm file=%v dir=%v", info.Mode().Perm(), dinfo.Mode().Perm())
	}
	out, err := s.Load(ref)
	if err != nil || out != in {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestStoreReadOnly(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	_ = os.Chmod(filepath.Join(dir, "sessions"), 0o500)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "sessions"), 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	s := Store{Dir: dir}
	if err := s.Save("x", Session{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("save: %v", err)
	}
	if _, err := s.Lock(context.Background(), "x"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("lock: %v", err)
	}
	if s.Writable() {
		t.Fatal("Writable must be false")
	}
}

func TestLockIsExclusive(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var inside, maxInside int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := s.Lock(context.Background(), "r")
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&inside, 1)
			for {
				m := atomic.LoadInt32(&maxInside)
				if n <= m || atomic.CompareAndSwapInt32(&maxInside, m, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inside, -1)
			unlock()
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Fatalf("max concurrent holders = %d", maxInside)
	}
}
