package app

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestProjectSpecStrictAndStableOrder(t *testing.T) {
	input := `[[story_status]]
name = "In revision"
color = "#8E44AD"
closed = false
after = "In progress"
[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste da versão entregue"
`
	spec, err := ParseProjectSpec(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	got, err := OrderStatuses([]string{"New", "In progress", "Done", "Extra"}, spec.StoryStatus)
	want := []string{"New", "In progress", "In revision", "Done", "Extra"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %v", got, err)
	}
	if spec.StoryField[0].Description != "Registro de teste da versão entregue" {
		t.Fatalf("description: %+v", spec.StoryField)
	}
}

func TestParseProjectSpecRefusals(t *testing.T) {
	for name, input := range map[string]string{
		"unknown key":         "unknown = true",
		"unknown status key":  "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nposition = 1",
		"bad color":           "[[story_status]]\nname = \"A\"\ncolor = \"red\"",
		"short color":         "[[story_status]]\nname = \"A\"\ncolor = \"#000\"",
		"no color":            "[[story_status]]\nname = \"A\"",
		"empty name":          "[[story_status]]\nname = \" \"\ncolor = \"#000000\"",
		"self reference":      "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nafter = \"A\"",
		"duplicate status":    "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\n[[story_status]]\nname = \"A\"\ncolor = \"#000000\"",
		"duplicate field":     "[[story_field]]\nname = \"F\"\ntype = \"text\"\n[[story_field]]\nname = \"F\"\ntype = \"date\"",
		"field type":          "[[story_field]]\nname = \"F\"\ntype = \"number\"",
		"field with =":        "[[story_field]]\nname = \"a=b\"\ntype = \"text\"",
		"closed as string":    "[[story_status]]\nname = \"A\"\ncolor = \"#000000\"\nclosed = \"no\"",
		"not TOML":            "[[story_status]\n",
		"status case":         "[[story_status]]\nname = \"ReviewCase\"\ncolor = \"#000000\"\n[[story_status]]\nname = \"reviewcase\"\ncolor = \"#000000\"",
		"status unicode case": "[[story_status]]\nname = \"Ação\"\ncolor = \"#000000\"\n[[story_status]]\nname = \"AÇÃO\"\ncolor = \"#000000\"",
		"field case":          "[[story_field]]\nname = \"ReviewCase\"\ntype = \"text\"\n[[story_field]]\nname = \"reviewcase\"\ntype = \"text\"",
	} {
		if _, err := ParseProjectSpec(strings.NewReader(input)); err == nil || exitOf(err) != 2 {
			t.Errorf("%s: %v", name, err)
		}
	}
	spec, err := ParseProjectSpec(strings.NewReader(""))
	if err != nil || len(spec.StoryStatus)+len(spec.StoryField) != 0 {
		t.Fatalf("empty: %+v %v", spec, err)
	}
}

func TestOrderStatuses(t *testing.T) {
	current := []string{"New", "In progress", "Ready for test", "Done"}
	for name, c := range map[string]struct {
		specs []StatusSpec
		want  []string
	}{
		"new without after goes last in file order": {
			[]StatusSpec{{Name: "X"}, {Name: "Y"}}, []string{"New", "In progress", "Ready for test", "Done", "X", "Y"}},
		"after a status declared later in the file": {
			[]StatusSpec{{Name: "B", After: "A"}, {Name: "A", After: "New"}}, []string{"New", "A", "B", "In progress", "Ready for test", "Done"}},
		"chain of three": {
			[]StatusSpec{{Name: "A", After: "Done"}, {Name: "B", After: "A"}, {Name: "C", After: "B"}}, []string{"New", "In progress", "Ready for test", "Done", "A", "B", "C"}},
		"two after the same keep file order": {
			[]StatusSpec{{Name: "Y", After: "New"}, {Name: "X", After: "New"}}, []string{"New", "Y", "X", "In progress", "Ready for test", "Done"}},
		"existing without after stays": {
			[]StatusSpec{{Name: "Done"}}, current},
		"existing moved by after": {
			[]StatusSpec{{Name: "New", After: "Done"}}, []string{"In progress", "Ready for test", "Done", "New"}},
		"after an external status": {
			[]StatusSpec{{Name: "In revision", After: "In progress"}, {Name: "Waiting", After: "In revision"}},
			[]string{"New", "In progress", "In revision", "Waiting", "Ready for test", "Done"}},
	} {
		got, err := OrderStatuses(current, c.specs)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	for name, c := range map[string]struct {
		current []string
		specs   []StatusSpec
	}{
		"cycle":            {nil, []StatusSpec{{Name: "A", After: "B"}, {Name: "B", After: "A"}}},
		"cycle of three":   {[]string{"A"}, []StatusSpec{{Name: "A", After: "C"}, {Name: "B", After: "A"}, {Name: "C", After: "B"}}},
		"self reference":   {nil, []StatusSpec{{Name: "A", After: "A"}}},
		"missing":          {[]string{"A"}, []StatusSpec{{Name: "B", After: "missing"}}},
		"duplicate remote": {[]string{"A", "A"}, nil},
		"duplicate spec":   {nil, []StatusSpec{{Name: "A"}, {Name: "A"}}},
	} {
		if _, err := OrderStatuses(c.current, c.specs); err == nil || exitOf(err) != 2 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func remoteStatuses() []Object {
	return []Object{
		{"id": 1, "name": "New", "order": 1, "color": "#70728F", "is_closed": false},
		{"id": 3, "name": "In progress", "order": 3, "color": "#E47C40", "is_closed": false},
		{"id": 4, "name": "Ready for test", "order": 4, "color": "#E4CE40", "is_closed": false},
		{"id": 5, "name": "Done", "order": 5, "color": "#A8E440", "is_closed": true},
	}
}

func remoteFields() []Object {
	return []Object{{"id": 9, "name": "Executor", "type": "text", "description": "who"}}
}

func kinds(actions []Action) string {
	out := []string{}
	for _, a := range actions {
		out = append(out, a.Kind+":"+a.Name)
	}
	return strings.Join(out, ",")
}

func TestBuildProjectPlan(t *testing.T) {
	spec := ProjectSpec{
		StoryStatus: []StatusSpec{{Name: "Waiting for deployment", Color: "#3498DB"}, {Name: "Done", Color: "#a8e440", Closed: true}},
		StoryField:  []FieldSpec{{Name: "Testado em staging", Type: "checkbox"}, {Name: "Executor", Type: "text", Description: "who"}},
	}
	plan, err := BuildProjectPlan(spec, remoteStatuses(), remoteFields())
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(plan.Actions); got != "create_status:Waiting for deployment,create_field:Testado em staging" {
		t.Fatalf("actions: %s", got)
	}
	if len(plan.Drift) != 0 || len(plan.Unmanaged) != 3 {
		t.Fatalf("drift %v unmanaged %v", plan.Drift, plan.Unmanaged)
	}
	if plan.Actions[0].Body["is_closed"] != false || plan.Actions[1].Body["description"] != "" {
		t.Fatalf("bodies: %v", plan.Actions)
	}
	// The same file against the result plans nothing and keeps the extras.
	after := append(remoteStatuses(), Object{"id": 7, "name": "Waiting for deployment", "order": 6, "color": "#3498DB", "is_closed": false})
	fields := append(remoteFields(), Object{"id": 10, "name": "Testado em staging", "type": "checkbox", "description": ""})
	plan, err = BuildProjectPlan(spec, after, fields)
	if err != nil || len(plan.Actions) != 0 || len(plan.Drift) != 0 || len(plan.Unmanaged) != 3 {
		t.Fatalf("second plan: %+v %v", plan, err)
	}
	// Only fields declared: never a reorder.
	plan, _ = BuildProjectPlan(ProjectSpec{StoryField: spec.StoryField}, remoteStatuses(), remoteFields())
	if got := kinds(plan.Actions); got != "create_field:Testado em staging" {
		t.Fatalf("fields only: %s", got)
	}
	// Empty file: nothing to do, everything unmanaged.
	plan, _ = BuildProjectPlan(ProjectSpec{}, remoteStatuses(), remoteFields())
	if len(plan.Actions) != 0 || len(plan.Unmanaged) != 5 || plan.Drift == nil {
		t.Fatalf("empty: %+v", plan)
	}
}

func TestBuildProjectPlanReorder(t *testing.T) {
	spec := ProjectSpec{StoryStatus: []StatusSpec{
		{Name: "In revision", Color: "#8E44AD", After: "In progress"},
		{Name: "Waiting for deployment", Color: "#3498DB", After: "In revision"},
	}}
	plan, err := BuildProjectPlan(spec, remoteStatuses(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(plan.Actions); got != "create_status:In revision,create_status:Waiting for deployment,reorder_statuses:" {
		t.Fatalf("actions: %s", got)
	}
	want := []string{"New", "In progress", "In revision", "Waiting for deployment", "Ready for test", "Done"}
	if got := plan.Actions[2].Body["names"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v", got)
	}
	// After the last status: created at the end, no reorder.
	spec.StoryStatus[0].After = "Done"
	plan, _ = BuildProjectPlan(spec, remoteStatuses(), nil)
	if got := kinds(plan.Actions); got != "create_status:In revision,create_status:Waiting for deployment" {
		t.Fatalf("after the last: %s", got)
	}
}

func TestBuildProjectPlanDrift(t *testing.T) {
	for name, c := range map[string]struct {
		spec   ProjectSpec
		fields []string
	}{
		"color":       {ProjectSpec{StoryStatus: []StatusSpec{{Name: "New", Color: "#000000"}}}, []string{"color"}},
		"closed":      {ProjectSpec{StoryStatus: []StatusSpec{{Name: "Done", Color: "#A8E440"}}}, []string{"is_closed"}},
		"closed=true": {ProjectSpec{StoryStatus: []StatusSpec{{Name: "New", Color: "#70728F", Closed: true}}}, []string{"is_closed"}},
		"status case": {ProjectSpec{StoryStatus: []StatusSpec{{Name: "in progress", Color: "#E47C40"}}}, []string{"name"}},
		"type":        {ProjectSpec{StoryField: []FieldSpec{{Name: "Executor", Type: "date", Description: "who"}}}, []string{"type"}},
		"description": {ProjectSpec{StoryField: []FieldSpec{{Name: "Executor", Type: "text"}}}, []string{"description"}},
		"field case":  {ProjectSpec{StoryField: []FieldSpec{{Name: "executor", Type: "text", Description: "who"}}}, []string{"name"}},
	} {
		plan, err := BuildProjectPlan(c.spec, remoteStatuses(), remoteFields())
		if err != nil || len(plan.Drift) != 1 || len(plan.Actions) != 0 || !reflect.DeepEqual(plan.Drift[0]["fields"], c.fields) {
			t.Errorf("%s: %+v %v", name, plan, err)
		}
	}
}

func TestBuildProjectPlanRefusesCaseDuplicatesInTheSpec(t *testing.T) {
	for name, spec := range map[string]ProjectSpec{
		"statuses": {StoryStatus: []StatusSpec{{Name: "ReviewCase", Color: "#000000"}, {Name: "reviewcase", Color: "#000000"}}},
		"fields":   {StoryField: []FieldSpec{{Name: "ReviewCase", Type: "text"}, {Name: "REVIEWCASE", Type: "text"}}},
	} {
		if _, err := BuildProjectPlan(spec, remoteStatuses(), remoteFields()); err == nil || exitOf(err) != 2 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBuildProjectPlanRefusesDuplicateCatalogs(t *testing.T) {
	statuses := append(remoteStatuses(), Object{"id": 8, "name": "New", "order": 8})
	if _, err := BuildProjectPlan(ProjectSpec{}, statuses, nil); err == nil || exitOf(err) != 2 {
		t.Fatalf("statuses: %v", err)
	}
	fields := append(remoteFields(), Object{"id": 11, "name": "Executor", "type": "text"})
	if _, err := BuildProjectPlan(ProjectSpec{}, remoteStatuses(), fields); err == nil || exitOf(err) != 2 {
		t.Fatalf("fields: %v", err)
	}
	if _, err := BuildProjectPlan(ProjectSpec{}, []Object{{"id": 1}}, nil); err == nil {
		t.Fatal("status without name")
	}
}

func TestSortStatusesLikeTaiga(t *testing.T) {
	statuses := []Object{{"name": "b", "order": "10"}, {"name": "a", "order": "10"}, {"name": "c", "order": "9"}}
	SortStatuses(statuses)
	got := []any{statuses[0]["name"], statuses[1]["name"], statuses[2]["name"]}
	if !reflect.DeepEqual(got, []any{"c", "a", "b"}) {
		t.Fatalf("%v", got)
	}
}

func TestExampleProjectFile(t *testing.T) {
	file, err := os.Open("../../docs/examples/taiga-project.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	spec, err := ParseProjectSpec(file)
	if err != nil {
		t.Fatal(err)
	}
	statuses := append(remoteStatuses(), Object{"id": 6, "name": "Archived", "order": 6, "color": "#A9AABC", "is_closed": true})
	plan, err := BuildProjectPlan(spec, statuses, nil)
	if err != nil || len(plan.Actions) != 9 || plan.Actions[8].Kind != "reorder_statuses" {
		t.Fatalf("%s %v", kinds(plan.Actions), err)
	}
	want := []string{"New", "In progress", "In revision", "Ready for test", "Waiting for deployment", "Done", "Archived"}
	if got := plan.Actions[8].Body["names"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v", got)
	}
}
