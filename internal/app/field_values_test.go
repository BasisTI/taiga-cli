package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/taiga"
)

func TestParseFieldValuesAndMerge(t *testing.T) {
	for _, tc := range []struct {
		typ, raw string
		want     any
	}{
		{"text", "12", "12"}, {"text", "", ""}, {"text", "null", "null"}, {"text", "a=b", "a=b"},
		{"checkbox", "false", false}, {"checkbox", "true", true}, {"date", "2026-09-30", "2026-09-30"}, {"date", "2024-02-29", "2024-02-29"},
	} {
		got, err := ParseFieldValue(tc.typ, tc.raw)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%+v: %v %v", tc, got, err)
		}
	}
	for _, tc := range [][2]string{{"checkbox", "yes"}, {"checkbox", "True"}, {"checkbox", ""}, {"date", "2026-02-30"}, {"date", "30/09/2026"},
		{"date", ""}, {"date", "2026-9-30"}, {"date", "null"}, {"number", "1"}, {"dropdown", "a"}} {
		if _, err := ParseFieldValue(tc[0], tc[1]); exitOf(err) != 2 {
			t.Fatalf("accepted %v: %v", tc, err)
		}
	}
	current := Object{"27": "old", "28": false, "999": map[string]any{"unknown": true}, "30": json.Number("1.50")}
	got := MergeValues(current, Object{"27": "new"})
	if got["27"] != "new" || got["28"] != false || !reflect.DeepEqual(got["999"], current["999"]) || got["30"] != json.Number("1.50") {
		t.Fatalf("%v", got)
	}
	if current["27"] != "old" || len(current) != 4 {
		t.Fatal("mutated input")
	}
}

func TestValuePath(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{{"story", "userstories/custom-attributes-values/7"}, {"task", "tasks/custom-attributes-values/7"}} {
		if got, err := ValuePath(tc.kind, 7); err != nil || got != tc.want {
			t.Fatalf("%s: %s %v", tc.kind, got, err)
		}
	}
	if _, err := ValuePath("story", 0); exitOf(err) != 2 {
		t.Fatal("id 0 accepted")
	}
	if _, err := ValuePath("epic", 1); exitOf(err) != 2 {
		t.Fatal("epic accepted")
	}
}

// patchAPI answers a PATCH with the body sent and version+1, and records it.
type patchAPI struct {
	fakeAPI
	patches []taiga.Request
}

func (p *patchAPI) Do(ctx context.Context, r taiga.Request) (*taiga.Response, error) {
	if r.Method != "PATCH" {
		return p.fakeAPI.Do(ctx, r)
	}
	p.patches = append(p.patches, r)
	body := r.Body.(map[string]any)
	b, _ := json.Marshal(map[string]any{"attributes_values": body["attributes_values"], "version": ID(body["version"]) + 1, "task": 5})
	p.objects[r.Path] = string(b)
	return &taiga.Response{Status: 200, Body: b}, nil
}

func TestTaskFieldValuesUseTheTaskRoutes(t *testing.T) {
	api := &patchAPI{fakeAPI: fakeAPI{
		objects: map[string]string{
			"tasks/5":                          `{"id":5,"ref":18,"project":37,"version":4}`,
			"tasks/6":                          `{"id":6,"ref":19,"project":38,"version":1}`,
			"tasks/custom-attributes-values/5": `{"attributes_values":{"1":"old","2":true},"version":9,"task":5}`,
		},
		lists: map[string]string{"task-custom-attributes": `[{"id":1,"name":"Horas","type":"text","project":37}]`},
	}}
	s := service(t, &api.fakeAPI)
	s.API = api
	if _, err := s.FieldValues(context.Background(), "task", 6); exitOf(err) != 2 {
		t.Fatalf("task of another project: %v", err)
	}
	got, err := s.SetFieldValues(context.Background(), "task", 5, []string{"Horas=8h"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.patches) != 1 || api.patches[0].Path != "tasks/custom-attributes-values/5" {
		t.Fatalf("%+v", api.patches)
	}
	body := api.patches[0].Body.(map[string]any)
	if ID(body["version"]) != 9 || !reflect.DeepEqual(body["attributes_values"], Object{"1": "8h", "2": true}) {
		t.Fatalf("body %v", body)
	}
	if v := got.(Object); ID(v["version"]) != 10 {
		t.Fatalf("%v", v)
	}
}
