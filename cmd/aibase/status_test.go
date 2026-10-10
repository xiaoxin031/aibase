package main

import (
	"bytes"
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/audit"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusAndConfigJSON(t *testing.T) {
	h := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, h)
	}
	invoke := func(args ...string) map[string]any {
		t.Helper()
		var out, err bytes.Buffer
		if execute(args, &out, &err) != 0 {
			t.Fatal(out.String(), err.String())
		}
		var v map[string]any
		if e := json.Unmarshal(out.Bytes(), &v); e != nil {
			t.Fatal(out.String(), e)
		}
		return v
	}
	cfg := invoke("config", "inject", "false", "--json")
	if cfg["inject"] != false {
		t.Fatal(cfg)
	}
	out, e := sessionHook(nil)
	if e != nil || out != "" {
		t.Fatal(out, e)
	}
	audit.Record(h, "codex", "SessionStart", 100, 0)
	audit.Record(h, "codex", "SessionStart", 0, 0)
	status := invoke("status", "--json")
	for _, k := range []string{"clients", "memory", "sessions", "last_injection", "doctor", "config"} {
		if _, ok := status[k]; !ok {
			t.Fatal(k)
		}
	}
	if status["last_injection"].(map[string]any)["bytes"] != float64(100) {
		t.Fatal(status)
	}
	cfg = invoke("config", "autosave", "false", "--json")
	if cfg["review_every"] != float64(-1) {
		t.Fatal(cfg)
	}
	cfg = invoke("config", "autosave", "true", "--json")
	if cfg["review_every"] != float64(8) {
		t.Fatal(cfg)
	}
	b, _ := os.ReadFile(filepath.Join(h, "config.json"))
	if !strings.Contains(string(b), `"inject": false`) {
		t.Fatal(string(b))
	}
}
