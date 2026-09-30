package cli

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `}`))
	return "eyJhbGciOiJIUzI1NiJ9." + payload + ".sig"
}

// runInRepo runs the CLI from a directory holding the given .taiga.toml.
func runInRepo(t *testing.T, env map[string]string, repoToml string, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".taiga.toml"), []byte(repoToml), 0o600); err != nil {
		t.Fatal(err)
	}
	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	var out, errOut bytes.Buffer
	a := &App{In: strings.NewReader(""), Out: &out, Err: &errOut, Env: func(k string) string { return env[k] }, Cwd: dir}
	code := a.Run(args)
	return out.String(), errOut.String(), code
}
