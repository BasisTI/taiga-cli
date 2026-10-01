package app

import "testing"

func TestFieldPathsAndValidation(t *testing.T) {
	for kind, want := range map[string]string{"story": "userstory-custom-attributes", "task": "task-custom-attributes"} {
		got, err := FieldPath(kind)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v", kind, got, err)
		}
	}
	for _, kind := range []string{"issue", "epic", "", "Story"} {
		if _, err := FieldPath(kind); exitOf(err) != 2 {
			t.Fatalf("kind %q: %v", kind, err)
		}
	}
	for _, typ := range []string{"text", "date", "checkbox"} {
		if err := ValidateField("Testado em staging", typ); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	// Taiga also has multiline, richtext, url, dropdown and number: not validated yet.
	for _, tc := range [][2]string{{"", "text"}, {" ", "date"}, {"x", "unknown"}, {"x", "number"}, {"x", "dropdown"}, {"x", ""}, {"Notas=interno", "text"},
		{"12345678901234567890123456789012345678901234567890123456789012345", "text"}} {
		if err := ValidateField(tc[0], tc[1]); exitOf(err) != 2 {
			t.Fatalf("accepted %v: %v", tc, err)
		}
	}
	if err := ValidateField("1234567890123456789012345678901234567890123456789012345678901234", "text"); err != nil {
		t.Fatalf("64 characters: %v", err)
	}
	if err := ValidateField("ção", "text"); err != nil {
		t.Fatal(err)
	}
}
