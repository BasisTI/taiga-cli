package cli

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

// closedURL returns a loopback URL on which nothing listens, so requests fail with a transport error.
func closedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr
}

func TestAPINetworkErrorNeverEchoesQueryValues(t *testing.T) {
	env := map[string]string{"TAIGA_URL": closedURL(t), "TAIGA_TOKEN": "tok"}
	cases := [][]string{
		{"api", "POST", "userstories", "--query", "access_token=SENTINEL_QUERY_SECRET", "--field", "a=1"},
		{"api", "POST", "userstories?access_token=SENTINEL_PATH_SECRET", "--field", "a=1"},
	}
	for _, args := range cases {
		_, errOut, code := runIn(t, env, "", args...)
		if code != 7 || !strings.Contains(errOut, `"code": "network_error"`) {
			t.Fatalf("%v: code=%d err=%s", args, code, errOut)
		}
		if strings.Contains(errOut, "SENTINEL") {
			t.Fatalf("%v: query value leaked into the envelope: %s", args, errOut)
		}
	}
}

func TestAPIErrorStageNeverEchoesQueryValuesFromPath(t *testing.T) {
	srv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	_, errOut, code := runIn(t, env, "", "api", "GET", "userstories?access_token=SENTINEL_PATH_SECRET")
	if code != 5 || strings.Contains(errOut, "SENTINEL") || !strings.Contains(errOut, `"stage": "GET userstories"`) {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}
