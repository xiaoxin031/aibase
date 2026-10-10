package skills

import (
	"strings"
	"testing"
)

func TestReadableSkills(t *testing.T) {
	s := Skill{Name: "demo", Description: "Useful\nworkflow", Body: "Instructions", Missing: []string{"git"}}
	got := ListText([]Skill{s})
	if got != "demo: Useful workflow [missing: git]" {
		t.Fatal(got)
	}
	if !strings.Contains(Text(s), "\n\nInstructions") {
		t.Fatal(Text(s))
	}
}
