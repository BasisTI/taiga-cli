package app

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
)

func TestSwimlanesMarksDefault(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{"swimlanes": `[{"id":2,"name":"B","order":20,"project":37},{"id":1,"name":"A","order":10,"project":37},{"id":9,"name":"X","order":1,"project":38}]`}}
	s := service(t, f)
	s.Project["default_swimlane"] = json.Number("2")
	lanes, err := s.Swimlanes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(lanes)
	if string(b) != `[{"id":1,"is_default":false,"name":"A","order":10,"project":37},{"id":2,"is_default":true,"name":"B","order":20,"project":37}]` {
		t.Fatalf("%s", b)
	}
	// The catalog itself is not changed: the flag lives only in the returned copies.
	if items, _ := s.Catalog(context.Background(), "swimlanes"); items[0]["is_default"] != nil {
		t.Fatalf("catalog changed: %v", items[0])
	}
}

func TestSwimlanesWithoutDefaultOrEntries(t *testing.T) {
	f := &fakeAPI{lists: map[string]string{"swimlanes": `[]`}}
	s := service(t, f)
	s.Project["default_swimlane"] = nil
	lanes, err := s.Swimlanes(context.Background())
	if b, _ := json.Marshal(lanes); err != nil || string(b) != `[]` {
		t.Fatalf("%s %v", b, err)
	}
}

func TestStoriesSwimlaneFilterIsLocalAndRemote(t *testing.T) {
	// The fake ignores every parameter, as Taiga ignores "swimlane": the local filter must hold.
	f := &fakeAPI{lists: map[string]string{"userstories": `[
		{"id":1,"ref":1,"project":37,"swimlane":10,"tags":[]},
		{"id":2,"ref":2,"project":37,"swimlane":11,"tags":[]},
		{"id":3,"ref":3,"project":37,"swimlane":null,"tags":[]},
		{"id":4,"ref":4,"project":37,"tags":[]}]`}}
	s := service(t, f)
	for value, want := range map[string]string{"11": "[2]", "null": "[3,4]", "99": "[]"} {
		got, err := s.Stories(context.Background(), url.Values{"swimlane": {value}})
		if err != nil {
			t.Fatal(err)
		}
		refs := []int64{}
		for _, o := range got {
			refs = append(refs, ID(o["ref"]))
		}
		if b, _ := json.Marshal(refs); string(b) != want {
			t.Errorf("swimlane=%s: got %s want %s", value, b, want)
		}
		if q := f.queries[len(f.queries)-1]; q.Get("swimnlane") != value || q.Has("swimlane") {
			t.Errorf("swimlane=%s: query %v", value, q)
		}
	}
}
