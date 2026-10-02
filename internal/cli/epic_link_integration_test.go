//go:build integration

package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// epicProject is a disposable project with epics enabled and three epics; it returns the admin
// and svc environments and the epic refs by subject prefix.
func epicProject(t *testing.T, name string) (map[string]string, map[string]string, map[string]string) {
	t.Helper()
	env, svc, pid := freshProject(t, name)
	storyJSON(t, env, "", "api", "PATCH", "projects/"+pid, "-F", "is_epics_activated=true")
	refs := map[string]string{}
	for _, s := range []string{"A", "B", "C"} {
		e := storyJSON(t, env, "", "api", "POST", "epics", "-F", "project="+pid, "-f", "subject="+s+" epic")
		refs[s] = fmt.Sprint(e["ref"])
	}
	return env, svc, refs
}

// storyEpicRefs reads the story through the CLI and returns its epic refs, sorted.
func storyEpicRefs(t *testing.T, env map[string]string, ref string) string {
	t.Helper()
	st := storyJSON(t, env, "", "story", "get", ref)
	list, _ := st["epics"].([]any)
	out := []string{}
	for _, e := range list {
		out = append(out, fmt.Sprint(e.(map[string]any)["ref"]))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func sortedRefs(refs ...string) string {
	sort.Strings(refs)
	return strings.Join(refs, ",")
}

func TestIntegrationEpicLinkAndReplace(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	_, svc, e := epicProject(t, "cli-test-epic-links-"+suffix)

	// story create --epic links the new story.
	st := storyJSON(t, svc, "", "story", "create", "--subject", "linked "+suffix, "--epic", e["A"])
	ref := fmt.Sprint(st["ref"])
	if got := storyEpicRefs(t, svc, ref); got != e["A"] {
		t.Fatalf("after create --epic: %s", got)
	}

	// epic link adds; the same link again is a no-op.
	r := storyJSON(t, svc, "", "epic", "link", e["B"], ref)
	if r["changed"] != true || r["linked"] != true {
		t.Fatalf("link: %v", r)
	}
	if r := storyJSON(t, svc, "", "epic", "link", e["B"], ref); r["changed"] != false {
		t.Fatalf("repeated link: %v", r)
	}
	if got := storyEpicRefs(t, svc, ref); got != sortedRefs(e["A"], e["B"]) {
		t.Fatalf("after link: %s", got)
	}

	// --replace-epic without --confirm-delete: refused, also with --dry-run; nothing changes.
	for _, extra := range [][]string{nil, {"--dry-run"}} {
		_, errOut, code := runIn(t, svc, "", append([]string{"story", "update", ref, "--replace-epic", e["C"]}, extra...)...)
		if code != 2 || !strings.Contains(errOut, "delete_not_confirmed") {
			t.Fatalf("replace without confirm %v: %d %s", extra, code, errOut)
		}
	}
	plan := storyJSON(t, svc, "", "story", "update", ref, "--replace-epic", e["C"], "--confirm-delete", "--dry-run")
	if reqs, _ := plan["requests"].([]any); len(reqs) != 3 || reqs[0].(map[string]any)["method"] != "POST" {
		t.Fatalf("plan: %v", plan)
	}
	if got := storyEpicRefs(t, svc, ref); got != sortedRefs(e["A"], e["B"]) {
		t.Fatalf("after dry-run: %s", got)
	}

	// The replacement leaves only the new epic, and running it again changes nothing.
	for i := 0; i < 2; i++ {
		st = storyJSON(t, svc, "", "story", "update", ref, "--subject", "replaced "+suffix, "--replace-epic", e["C"], "--confirm-delete")
		if got := storyEpicRefs(t, svc, ref); got != e["C"] || st["subject"] != "replaced "+suffix {
			t.Fatalf("replace run %d: %s %v", i, got, st["subject"])
		}
	}
	r = storyJSON(t, svc, "", "epic", "link", e["A"], ref, "--replace", "--confirm-delete")
	if fmt.Sprint(r["epics"]) != "["+e["A"]+"]" || fmt.Sprint(r["removed"]) != "["+e["C"]+"]" {
		t.Fatalf("epic link --replace: %v", r)
	}
	detail := storyJSON(t, svc, "", "epic", "get", e["A"])
	if !strings.Contains(fmt.Sprint(detail["user_stories"]), "ref:"+ref) {
		t.Fatalf("epic get: %v", detail["user_stories"])
	}

	// An epic that is not in the project: not found, nothing created.
	before := storyRefs(t, svc)
	_, errOut, code := runIn(t, svc, "", "story", "create", "--subject", "never "+suffix, "--epic", "9999")
	if code != 5 || storyRefs(t, svc) != before {
		t.Fatalf("unknown epic: %d %s", code, errOut)
	}
}

// dropDelete is a proxy to Taiga that loses the answer of the first DELETE: with forward, the
// DELETE reaches Taiga and only the answer is lost; without, the connection drops before it.
func dropDelete(t *testing.T, env map[string]string, forward bool) (map[string]string, *int) {
	t.Helper()
	target, err := url.Parse(env["TAIGA_URL"])
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			if deletes == 1 {
				if forward {
					proxy.ServeHTTP(httptest.NewRecorder(), r)
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["TAIGA_URL"] = srv.URL
	return out, &deletes
}

func TestIntegrationReplaceEpicDeleteLost(t *testing.T) {
	suffix := fmt.Sprint(time.Now().UnixNano())
	_, svc, e := epicProject(t, "cli-test-epic-lost-"+suffix)
	for _, forward := range []bool{true, false} {
		ref := fmt.Sprint(storyJSON(t, svc, "", "story", "create", "--subject", fmt.Sprintf("lost %v %s", forward, suffix), "--epic", e["A"])["ref"])
		proxied, deletes := dropDelete(t, svc, forward)
		_, errOut, code := runIn(t, proxied, "", "epic", "link", e["B"], ref, "--replace", "--confirm-delete")
		if code != 1 || !strings.Contains(errOut, "epic_replace_incomplete") || *deletes != 1 {
			t.Fatalf("forward=%v: %d %s (deletes %d)", forward, code, errOut, *deletes)
		}
		want := e["B"]
		if !forward {
			want = sortedRefs(e["A"], e["B"])
		}
		if got := storyEpicRefs(t, svc, ref); got != want {
			t.Fatalf("forward=%v after the lost DELETE: %s, want %s (the new link must be in)", forward, got, want)
		}
		// Running the same command again converges.
		r := storyJSON(t, proxied, "", "epic", "link", e["B"], ref, "--replace", "--confirm-delete")
		if got := storyEpicRefs(t, svc, ref); got != e["B"] {
			t.Fatalf("forward=%v rerun: %s %v", forward, got, r)
		}
		if r["linked"] != false || r["changed"] != !forward {
			t.Fatalf("forward=%v rerun result: %v", forward, r)
		}
	}
}
