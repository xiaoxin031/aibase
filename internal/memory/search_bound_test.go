package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchRetainsRankingCountAndStableTies(t *testing.T) {
	h := t.TempDir()
	t.Setenv("AIBASE_HOME", h)
	lines := `{"ts":"2026-01-01","text":"needle earliest"}
{"ts":"2026-03-01","text":"needle first-tie"}
{"ts":"2026-03-01","text":"needle second-tie"}
{"ts":"2026-02-01","text":"needle needle highest"}
{"ts":"2026-04-01","text":"unrelated"}
broken JSON
`
	if err := os.WriteFile(filepath.Join(h, "history.jsonl"), []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := search("needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := "4 matches, showing 2 (full note: aibase show <slug>)\n\n[log 2026-02-01]\nneedle needle highest\n\n[log 2026-03-01]\nneedle first-tie\n"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	got, err = search("needle", 10)
	if err != nil || strings.Count(got, "[log ") != 4 || strings.Contains(got, "showing") {
		t.Fatalf("full result: %q, %v", got, err)
	}
}
