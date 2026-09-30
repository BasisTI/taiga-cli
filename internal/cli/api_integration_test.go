//go:build integration

package cli

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestIntegrationAPIProjectsAndUsersMe(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token}
	out, errOut, code := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(out), &p); err != nil || p["slug"] != testtaiga.ProjectSlug {
		t.Fatalf("project: %s", out)
	}
	out, _, code = runIn(t, env, "", "api", "GET", "projects", "--paginate")
	var list []any
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) == 0 {
		t.Fatalf("paginate: code=%d out=%s", code, out)
	}
	_, errOut, code = runIn(t, env, "", "api", "GET", "userstories/999999")
	if code != 5 {
		t.Fatalf("not found must exit 5: %d %s", code, errOut)
	}
}

func TestIntegrationAutoVersionOnStory(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token}
	out, _, _ := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	var p map[string]any
	_ = json.Unmarshal([]byte(out), &p)
	pid := strconv.Itoa(int(p["id"].(float64)))
	out, errOut, code := runIn(t, env, "", "api", "POST", "userstories", "--field", "project="+pid, "--field", "subject=auto-version")
	if code != 0 {
		t.Fatalf("create story: %d %s", code, errOut)
	}
	var us map[string]any
	_ = json.Unmarshal([]byte(out), &us)
	sid := strconv.Itoa(int(us["id"].(float64)))
	desc := "integração \"auto-version\"\ncom quebra\t\u0001"
	_, errOut, code = runIn(t, env, "", "api", "PATCH", "userstories/"+sid, "--field", "description="+desc, "--auto-version")
	if code != 0 {
		t.Fatalf("patch exit %d: %s", code, errOut)
	}
	out, _, _ = runIn(t, env, "", "api", "GET", "userstories/"+sid)
	_ = json.Unmarshal([]byte(out), &us)
	if us["description"] != desc {
		t.Fatalf("description = %q", us["description"])
	}
}
