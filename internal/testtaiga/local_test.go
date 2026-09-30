package testtaiga

import (
	"os"
	"os/exec"
	"testing"
)

var localURLCases = []struct {
	url    string
	accept bool
}{
	{"http://localhost:8000", true},
	{"http://LOCALHOST:8000", true},
	{"http://127.0.0.1:8000", true},
	{"http://127.1.2.3:8000", true},
	{"http://[::1]:8000", true},
	{"http://agile:8000", false},
	{"http://taiga-back:8000", false},
	{"https://agile.basis.com.br", false},
	{"http://user@agile.basis.com.br", false},
	{"http://localhost@evil.com", false},
	{"http://10.0.0.5:8000", false},
	{"http://127.0.0.256:18001", false},
	{"http://127.1.2.999:8000", false},
	{"http://127.0.0:8000", false},
	{"http://127.0.0.1.5:8000", false},
	{"http://127.0.0.01:8000", false},
	{"http://127.255.255.255:8000", true},
	{"http://[2001:db8::1]:8000", false},
	{"http://", false},
	{"http:///x", false},
}

func TestCheckLocal(t *testing.T) {
	for _, tt := range localURLCases {
		if err := checkLocal(tt.url); (err == nil) != tt.accept {
			t.Errorf("checkLocal(%q) err=%v, accept=%v", tt.url, err, tt.accept)
		}
	}
}

// TestSeedCheckURL runs the same cases through the guard in scripts/taiga-seed.
// --check-url stops right after the guard, before any network call.
func TestSeedCheckURL(t *testing.T) {
	for _, tt := range localURLCases {
		cmd := exec.Command("bash", "../../scripts/taiga-seed", "--check-url")
		cmd.Env = append(os.Environ(), "TAIGA_TEST_URL="+tt.url)
		out, err := cmd.CombinedOutput()
		if (err == nil) != tt.accept {
			t.Errorf("taiga-seed --check-url with %q: err=%v, accept=%v, output=%s", tt.url, err, tt.accept, out)
		}
	}
}
