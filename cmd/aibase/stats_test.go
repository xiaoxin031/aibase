package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaoxin031/aibase/internal/audit"
)

func TestStats(t *testing.T) {
	h := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, h)
	}
	var out, errb bytes.Buffer
	if execute([]string{"add", "user", "prefers short answers"}, &out, &errb) != 0 {
		t.Fatal(errb.String())
	}
	audit.Record(h, "codex", "SessionStart", 100, 0)
	audit.Record(h, "mcp", "SessionStart", 100, 0)
	audit.Record(h, "claude", "Stop", 900, 0)
	audit.Record(h, "claude", "Stop", 0, 0)
	out.Reset()
	if execute([]string{"stats", "--json"}, &out, &errb) != 0 {
		t.Fatal(errb.String())
	}
	var r statsReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(out.String(), err)
	}
	if r.Today["sessions"] != 1 || r.Today["reviews"] != 1 || r.Today["saves"] != 1 {
		t.Fatalf("today: %v", r.Today)
	}
	if len(r.Activity.Days) != 7 || len(r.Memory) == 0 || r.DataDir != h {
		t.Fatalf("%+v", r)
	}
	out.Reset()
	if execute([]string{"stats"}, &out, &errb) != 0 || !strings.Contains(out.String(), "Tokens today: 0") {
		t.Fatal(out.String())
	}
}
