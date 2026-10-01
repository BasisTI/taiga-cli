//go:build integration

package taiga

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func hasPermission(t *testing.T, c *Client, project int64, perm string) bool {
	t.Helper()
	var perms []string
	p := probeDo(t, c, "GET", fmt.Sprintf("projects/%d", project), nil, nil)
	if err := json.Unmarshal(p["my_permissions"], &perms); err != nil {
		t.Fatalf("my_permissions %s: %v", p["my_permissions"], err)
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func statusOrder(t *testing.T, c *Client, id int64) int64 {
	t.Helper()
	return probeDo(t, c, "GET", fmt.Sprintf("userstory-statuses/%d", id), nil, nil).int("order")
}

// TestProbeStatusContract records the status contract of Taiga 6.7 that project apply relies on,
// and the gate of US #249: status order has no optimistic concurrency.
func TestProbeStatusContract(t *testing.T) {
	c := probeClient(t)
	project := ensureProject(t, c, "cli-test-probe-statuses")
	svcID := userID(t, c, project, testtaiga.ServiceUser)
	ensureMember(t, c, project, svcID, testtaiga.ServiceEmail)
	q := url.Values{"project": {fmt.Sprint(project)}}
	suffix := fmt.Sprint(time.Now().UnixNano())

	// Catalog shape: no version on statuses, nor on the project.
	for path, keys := range map[string][]string{
		"userstory-statuses": {"id", "name", "slug", "order", "is_closed", "is_archived", "color", "wip_limit", "project"},
		"task-statuses":      {"id", "name", "slug", "order", "is_closed", "color", "project"},
	} {
		list := probeList(t, c, path, q)
		if len(list) == 0 {
			t.Fatalf("%s: empty", path)
		}
		for _, k := range keys {
			if _, ok := list[0][k]; !ok {
				t.Fatalf("%s: no %q in %v", path, k, list[0])
			}
		}
		if _, ok := list[0]["version"]; ok {
			t.Fatalf("%s has a version: %v", path, list[0])
		}
	}
	if p := probeDo(t, c, "GET", fmt.Sprintf("projects/%d", project), nil, nil); string(p["version"]) != "null" && p["version"] != nil {
		t.Fatalf("project version: %s", p["version"])
	}

	// POST: without order the status is created at 10, not at the end; order is taken as sent;
	// a version is ignored; the same name is refused on "name"; another case is another name.
	a := probeDo(t, c, "POST", "userstory-statuses", nil, map[string]any{"project": project, "name": "probe a " + suffix, "color": "#8E44AD", "is_closed": false})
	if a.int("order") != 10 || string(a["is_closed"]) != "false" || string(a["color"]) != `"#8E44AD"` {
		t.Fatalf("created: %v", a)
	}
	b := probeDo(t, c, "POST", "userstory-statuses", nil, map[string]any{"project": project, "name": "probe b " + suffix, "color": "#000000", "order": 4242, "version": 0})
	if b.int("order") != 4242 {
		t.Fatalf("explicit order: %v", b)
	}
	if status, body := probeErr(t, c, "POST", "userstory-statuses", map[string]any{"project": project, "name": "probe a " + suffix, "color": "#000000"}); status != 400 || body["name"] == nil {
		t.Fatalf("duplicate: %d %v", status, body)
	}
	probeDo(t, c, "POST", "userstory-statuses", nil, map[string]any{"project": project, "name": strings.ToUpper("probe a " + suffix), "color": "#000000"})

	// No OCC on order: a PATCH with any version, or none, is applied, and so is the bulk route.
	id := a.int("id")
	if o := probeDo(t, c, "PATCH", fmt.Sprintf("userstory-statuses/%d", id), nil, map[string]any{"order": 99, "version": 12345}); o.int("order") != 99 {
		t.Fatalf("patch with a bogus version: %v", o)
	}
	if o := probeDo(t, c, "PATCH", fmt.Sprintf("userstory-statuses/%d", id), nil, map[string]any{"order": 98}); o.int("order") != 98 {
		t.Fatalf("patch without version: %v", o)
	}
	bulk := map[string]any{"project": project, "bulk_userstory_statuses": [][]int64{{id, 97}}, "version": 999}
	if _, err := c.Do(t.Context(), Request{Method: "POST", Path: "userstory-statuses/bulk_update_order", Body: bulk}); err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if statusOrder(t, c, id) != 97 {
		t.Fatal("bulk order not applied")
	}
	// The bulk route only touches statuses of the given project, silently.
	other := ensureProject(t, c, testtaiga.ProjectSlug)
	foreign := probeList(t, c, "userstory-statuses", url.Values{"project": {fmt.Sprint(other)}})[0]
	bulk = map[string]any{"project": project, "bulk_userstory_statuses": [][]int64{{foreign.int("id"), 555}}}
	if _, err := c.Do(t.Context(), Request{Method: "POST", Path: "userstory-statuses/bulk_update_order", Body: bulk}); err != nil {
		t.Fatalf("bulk foreign: %v", err)
	}
	if statusOrder(t, c, foreign.int("id")) == 555 {
		t.Fatal("bulk changed a status of another project")
	}

	// Permissions: admin_project_values is listed for the admin and not for a plain member, who
	// reads the catalog but gets 403 on every write.
	if !hasPermission(t, c, project, "admin_project_values") {
		t.Fatal("admin without admin_project_values")
	}
	svc := svcClient(t)
	if hasPermission(t, svc, project, "admin_project_values") {
		t.Fatal("svc has admin_project_values")
	}
	if len(probeList(t, svc, "userstory-statuses", q)) == 0 {
		t.Fatal("svc cannot read statuses")
	}
	for _, w := range []struct {
		method, path string
		body         any
	}{
		{"POST", "userstory-statuses", map[string]any{"project": project, "name": "svc " + suffix, "color": "#000000"}},
		{"PATCH", fmt.Sprintf("userstory-statuses/%d", id), map[string]any{"order": 1}},
		{"POST", "userstory-statuses/bulk_update_order", map[string]any{"project": project, "bulk_userstory_statuses": [][]int64{{id, 1}}}},
	} {
		if status, _ := probeErr(t, svc, w.method, w.path, w.body); status != 403 {
			t.Fatalf("svc %s %s: %d", w.method, w.path, status)
		}
	}
}
