package doctor

import (
	"github.com/xiaoxin031/aibase/internal/clients"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyBoundedSecretReport(t *testing.T) {
	h := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, h)
	}
	os.WriteFile(filepath.Join(h, "USER.md"), []byte("TOKEN=secret-value"), 0600)
	os.Mkdir(filepath.Join(h, "cache"), 0700)
	os.WriteFile(filepath.Join(h, "cache", "broken.json"), []byte("broken"), 0600)
	before, _ := os.ReadDir(h)
	out := Text(Run(h))
	after, _ := os.ReadDir(h)
	if len(before) != len(after) || strings.Contains(out, "secret-value") || !strings.Contains(out, "secrets") || !strings.Contains(out, "damaged") {
		t.Fatal(out)
	}
	for _, c := range Run(h) {
		if c.Status != "ok" && c.Status != "warn" && c.Status != "fail" {
			t.Fatal(c)
		}
	}
}

func TestOptionalClientsAndConfiguredTrust(t *testing.T) {
	h := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(key, h)
	}
	t.Setenv("PATH", "")
	t.Setenv("CODEX_HOME", filepath.Join(h, "codex"))
	for _, c := range Quick(h) {
		if strings.HasPrefix(c.Name, "client.") && c.Status != "ok" {
			t.Fatal(c)
		}
	}
	// Populate only isolated fixture configurations; no actual client is started.
	for _, name := range []string{"claude", "codex"} {
		if _, err := clients.Run([]string{name}, false); err != nil {
			t.Fatal(err)
		}
	}
	checks := map[string]Check{}
	for _, c := range Quick(h) {
		checks[c.Name] = c
	}
	if checks["client.claude"].Status != "ok" {
		t.Fatal(checks)
	}
	c := checks["client.codex"]
	if c.Status != "warn" || !strings.Contains(c.Detail, "trust unverified") || !strings.Contains(c.Fix, "/hooks") {
		t.Fatal(c)
	}
}
