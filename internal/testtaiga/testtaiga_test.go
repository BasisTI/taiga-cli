//go:build integration

package testtaiga

import "testing"

func TestCheckLocal(t *testing.T) {
	tests := []struct {
		url     string
		allow   bool
		wantErr bool
	}{
		{"http://localhost:8000", false, false},
		{"http://127.0.0.1:8000", false, false},
		{"http://[::1]:8000", false, false},
		{"http://taiga-back:8000", false, false},
		{"https://agile.basis.com.br", false, true},
		{"http://10.0.0.5:8000", false, true},
		{"http://user@agile.basis.com.br", false, true},
		{"https://agile.basis.com.br", true, false},
		{"", false, true},
		{"http://localhost@evil.com", false, true},
		{"http://[2001:db8::1]:8000", false, true},
		{"http://LOCALHOST:8000", false, false},
	}
	for _, tt := range tests {
		err := checkLocal(tt.url, tt.allow)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkLocal(%q, %v) err=%v, wantErr=%v", tt.url, tt.allow, err, tt.wantErr)
		}
	}
}
