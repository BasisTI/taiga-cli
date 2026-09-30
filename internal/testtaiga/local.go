package testtaiga

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// checkLocal accepts only loopback hosts: localhost, 127.0.0.0/8 and ::1.
// Tests must never write to a real Taiga, and there is no override.
func checkLocal(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("testtaiga: invalid TAIGA_TEST_URL %q", rawURL)
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("testtaiga: refusing TAIGA_TEST_URL host %q: only loopback (localhost, 127.0.0.0/8, ::1) is allowed", host)
}
