package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsCompatibilityAndSwitches(t *testing.T) {
	h := t.TempDir()
	t.Setenv("AIBASE_HOME", h)
	c, e := ReadSettings()
	if e != nil || !c.Inject || !c.Autosave || c.ReviewEvery != 8 {
		t.Fatal(c, e)
	}
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"limits":{"user":123},"custom":"keep","review_every":3}`), 0600)
	c, e = SetSetting("inject", false)
	if e != nil || c.Inject || c.ReviewEvery != 3 {
		t.Fatal(c, e)
	}
	b, _ := os.ReadFile(filepath.Join(h, "config.json"))
	if !strings.Contains(string(b), `"custom": "keep"`) {
		t.Fatal(string(b))
	}
	backups, _ := filepath.Glob(filepath.Join(h, "config.json.bak-*"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	c, e = SetSetting("autosave", false)
	if e != nil || c.Autosave || reviewEvery() != -1 {
		t.Fatal(c, e)
	}
	c, e = SetSetting("autosave", true)
	if e != nil || !c.Autosave || reviewEvery() != 8 {
		t.Fatal(c, e)
	}
	if InjectionEnabled() {
		t.Fatal("injection enabled")
	}
	os.WriteFile(filepath.Join(h, "config.json"), []byte("bad json"), 0600)
	if _, e = SetSetting("inject", true); e == nil {
		t.Fatal("damaged config overwritten")
	}
}
func TestRecentSessionsBoundedAndDeduplicated(t *testing.T) {
	t.Setenv("AIBASE_HOME", t.TempDir())
	var lines []string
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "f"} {
		b, _ := json.Marshal(logEntry{Session: id, Text: id})
		lines = append(lines, string(b))
	}
	os.WriteFile(historyPath(), []byte(strings.Join(lines, "\n")+"\n"), 0600)
	rows, e := RecentSessions()
	if e != nil || len(rows) != 5 || rows[0].Session != "f" || rows[4].Session != "b" {
		t.Fatal(rows, e)
	}
}
