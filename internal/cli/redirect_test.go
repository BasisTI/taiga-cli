package cli

import (
	"strings"
	"testing"
)

// Every curated write answered with a redirect (3xx) is an uncertain outcome (US #274): the
// client never follows it, and a proxy may have passed the request on. Applied or not, the write
// is sent once, the command never exits 7 (scripts repeat it) and never follows the Location.
// An update is confirmed by a re-read (exit 0 when applied); a creation is never a success
// (option B of 2026-10-03): it names what it finds.
func TestEveryCuratedWriteRedirectIsUncertain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request string
		args    []string
		setup   func(*storyFake)
		// applied and missing are the exit and the code expected when the write landed or not
		// (code "" with exit 0: success); found is in the error of an applied creation.
		appliedExit, missingExit int
		appliedCode, missingCode string
		found                    string
	}{
		{name: "story update", request: "PATCH userstories/6808", args: []string{"story", "update", "246", "--subject", "novo"},
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story update block", request: "PATCH userstories/6808", args: []string{"story", "update", "246", "--block", "esperando"},
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story update swimlane", request: "PATCH userstories/6808", args: []string{"story", "update", "246", "--swimlane", "Lane"},
			setup: func(f *storyFake) {
				f.swimlanes = []map[string]any{{"id": 21, "name": "Lane", "order": 1, "project": 37}}
			},
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story update assignees", request: "PATCH userstories/6808", args: []string{"story", "update", "246", "--add-assignee", "svc"},
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story close", request: "PATCH userstories/6808", args: []string{"story", "close", "246", "--status", "Done"},
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story field set", request: "PATCH userstories/custom-attributes-values/6808", args: []string{"story", "field", "set", "246", "Notas=x"},
			setup: func(f *storyFake) { g, _ := fieldFake(f.t); f.defs = g.defs },
			missingExit: 1, missingCode: "story_update_unconfirmed"},
		{name: "story comment", request: "PATCH userstories/6809", args: []string{"story", "comment", "247", "--body", "feito"},
			setup:       func(f *storyFake) { f.history = map[int64][]map[string]any{6809: {}} },
			appliedExit: 1, appliedCode: "comment_unconfirmed", missingExit: 1, missingCode: "comment_unconfirmed", found: "1 new comment(s)"},
		{name: "story create", request: "POST userstories", args: []string{"story", "create", "--subject", "nova"},
			appliedExit: 1, appliedCode: "story_create_unconfirmed", missingExit: 1, missingCode: "story_create_unconfirmed", found: "taiga story get 300"},
		{name: "story create --epic", request: "POST userstories", args: []string{"story", "create", "--subject", "nova", "--epic", "91"},
			appliedExit: 1, appliedCode: "story_create_unconfirmed", missingExit: 1, missingCode: "story_create_unconfirmed", found: "taiga story get 300"},
		{name: "epic link", request: "POST epics/9/related_userstories", args: []string{"epic", "link", "91", "246"},
			missingExit: 1, missingCode: "epic_link_unconfirmed"},
		{name: "field create", request: "POST userstory-custom-attributes", args: []string{"field", "create", "--kind", "story", "--name", "Nova", "--type", "text"},
			setup:       func(f *storyFake) { g, _ := fieldFake(f.t); f.defs = g.defs },
			appliedExit: 1, appliedCode: "field_create_unconfirmed", missingExit: 1, missingCode: "field_create_unconfirmed", found: "has a field with this name"},
		{name: "task create", request: "POST tasks", args: []string{"task", "create", "--story", "246", "--subject", "nova"},
			appliedExit: 1, appliedCode: "task_create_unconfirmed", missingExit: 1, missingCode: "task_create_unconfirmed", found: "taiga task get 301"},
		{name: "task update", request: "PATCH tasks/9100", args: []string{"task", "update", "250", "--subject", "novo"},
			missingExit: 1, missingCode: "task_update_unconfirmed"},
		{name: "task field set", request: "PATCH tasks/custom-attributes-values/9100", args: []string{"task", "field", "set", "250", "Horas=8h"},
			setup: func(f *storyFake) { g, _ := fieldFake(f.t); f.defs = g.defs },
			missingExit: 1, missingCode: "task_update_unconfirmed"},
		{name: "task comment", request: "PATCH tasks/9100", args: []string{"task", "comment", "250", "--body", "feito"},
			setup:       func(f *storyFake) { f.history = map[int64][]map[string]any{9100: {}} },
			appliedExit: 1, appliedCode: "comment_unconfirmed", missingExit: 1, missingCode: "comment_unconfirmed", found: "1 new comment(s)"},
	} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			for _, applied := range []bool{true, false} {
				f, calls := newStoryFake(t)
				if tc.setup != nil {
					tc.setup(f)
				}
				if applied {
					f.answer = map[string]int{tc.request: status}
				} else {
					f.fail[tc.request] = status
				}
				out, stderr, code := runIn(t, f.env(), "", tc.args...)
				wantExit, wantCode := tc.missingExit, tc.missingCode
				if applied {
					wantExit, wantCode = tc.appliedExit, tc.appliedCode
				}
				sent := 0
				for _, c := range writes(calls) {
					if c.method+" "+strings.TrimPrefix(c.path, "/api/v1/") == tc.request {
						sent++
					}
				}
				label := map[bool]string{true: "applied", false: "not applied"}[applied]
				switch {
				case code == 7:
					t.Errorf("%s %d %s: exit 7 after the write was sent: %s", tc.name, status, label, stderr)
				case code != wantExit || wantCode != "" && !strings.Contains(stderr, `"`+wantCode+`"`):
					t.Errorf("%s %d %s: exit %d, want %d %s: %s", tc.name, status, label, code, wantExit, wantCode, stderr)
				case sent != 1:
					t.Errorf("%s %d %s: %s sent %d times", tc.name, status, label, tc.request, sent)
				case code != 0 && out != "":
					t.Errorf("%s %d %s: stdout on failure: %s", tc.name, status, label, out)
				case applied && tc.found != "" && !strings.Contains(stderr, tc.found):
					t.Errorf("%s %d %s: %q not named: %s", tc.name, status, label, tc.found, stderr)
				case strings.Contains(stderr, "elsewhere.example"):
					t.Errorf("%s %d %s: Location echoed: %s", tc.name, status, label, stderr)
				}
				for _, c := range writes(calls) {
					if tc.name == "story create --epic" && strings.Contains(c.path, "related_userstories") {
						t.Errorf("%s %d %s: linked an unconfirmed story", tc.name, status, label)
					}
				}
			}
		}
	}
}

// An assignee PATCH answered with a redirect, applied, but with another write landing next to it
// (the re-read shows a version past the next one): Taiga cannot detect a concurrent change of
// assigned_to, so it is story_update_unconfirmed, not a success; --force-version skips the
// version check, as on the answered path.
func TestStoryAssigneesRedirectNextToAnotherWrite(t *testing.T) {
	for _, force := range []bool{false, true} {
		f, calls := newStoryFake(t)
		f.answer = map[string]int{"PATCH userstories/6808": 302}
		reads := 0
		f.onRead = func(s map[string]any) {
			if s["id"] == 6808 {
				if reads++; reads == 2 && !force || reads == 1 && force { // the re-read after the PATCH
					s["version"] = s["version"].(int) + 1
				}
			}
		}
		args := []string{"story", "update", "246", "--add-assignee", "svc"}
		if force {
			args = append(args, "--force-version")
		}
		_, stderr, code := runIn(t, f.env(), "", args...)
		if force && code != 0 || !force && (code != 1 || !strings.Contains(stderr, "story_update_unconfirmed") || !strings.Contains(stderr, "the version is 9, not 8")) || len(writes(calls)) != 1 {
			t.Fatalf("force=%v: %d %s", force, code, stderr)
		}
	}
}
