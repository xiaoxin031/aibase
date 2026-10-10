package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissiveFrontmatter(t *testing.T) {
	for _, requires := range []string{"  requires: [go, 'git', \"tool-missing-aibase\"]", "  requires:\n    - go\n    - git\n    - tool-missing-aibase", "  requires:\n    bins: [go, git, tool-missing-aibase]", "  requires:\n  - go\n  - git\n  - tool-missing-aibase"} {
		input := "---\nname: My_Skill:Demo\ndescription: '" + strings.Repeat("long ", 300) + "'\nargument-hint: [task]\nunknown:\n  list: [one, two]\nmetadata:\n" + requires + "\n  owner:\n    name: 'O''Brien'\n    info: |-\n      first\n      second\n---\nbody"
		s, e := Parse([]byte(input))
		if e != nil {
			t.Fatal(e)
		}
		if s.Name != "My_Skill:Demo" || len(s.Description) < 1024 || s.Metadata["owner.name"] != "O'Brien" || s.Metadata["owner.info"] != "first\nsecond" {
			t.Fatalf("%+v", s)
		}
		if s.Metadata["requires"] != "go,git,tool-missing-aibase" && s.Metadata["requires.bins"] != "go,git,tool-missing-aibase" {
			t.Fatal(s.Metadata)
		}
		isolated(t)
		dir := filepath.Join(Root(false), "Mixed_Case")
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(input), 0600); e != nil {
			t.Fatal(e)
		}
		got, e := Show("Mixed_Case", false)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(strings.Join(got.Missing, ","), "tool-missing-aibase") {
			t.Fatal(got)
		}
	}
}

func TestQuotedFrontmatterComments(t *testing.T) {
	s, err := Parse([]byte("---\nname: 'demo' # name\ndescription: \"Keep # inside\" # outside\nmetadata:\n  requires: [go, 'git'] # tools\n---\n"))
	if err != nil || s.Name != "demo" || s.Description != "Keep # inside" || s.Metadata["requires"] != "go,git" {
		t.Fatalf("%+v %v", s, err)
	}
}
