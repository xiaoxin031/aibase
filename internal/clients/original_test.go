package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model":"x","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"other"}]},{"hooks":[{"type":"command","command":"/old/aibase hook"}]}]}}`), 0o600)
	for range 2 {
		if _, err := addHooks(path, "/bin/aibase", "claude", true); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	start, _ := json.Marshal(quoteExe("/bin/aibase") + " hook --agent claude")
	stop, _ := json.Marshal(quoteExe("/bin/aibase") + " hook stop --agent claude")
	if strings.Count(s, string(start)) != 1 || strings.Count(s, string(stop)) != 1 ||
		strings.Contains(s, "/old/aibase") || !strings.Contains(s, `"other"`) || !strings.Contains(s, `"model"`) || strings.Count(s, "Bash(aibase:*)") != 1 {
		t.Fatalf("merge wrong:\n%s", b)
	}
	if baks, _ := filepath.Glob(path + ".bak-aibase-*"); len(baks) != 1 {
		t.Fatalf("want 1 backup, got %d", len(baks))
	}
}
