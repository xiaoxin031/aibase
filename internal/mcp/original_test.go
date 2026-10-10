package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/brief"
	"strings"
	"testing"
	"time"
)

func TestMCP(t *testing.T) {
	t.Setenv("AIBASE_HOME", t.TempDir())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_save","arguments":{"target":"memory","ops":[{"op":"add","text":"hello"}]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory_save","arguments":{"target":"memory","ops":[{"op":"remove","old":"nope"}]}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"bogus"}`,
	}, "\n")
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 responses, got %d:\n%s", len(lines), out.String())
	}
	var r []map[string]any
	for _, l := range lines {
		var m map[string]any
		json.Unmarshal([]byte(l), &m)
		r = append(r, m)
	}
	if r[0]["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Error("protocol version not echoed")
	}
	if n := len(r[1]["result"].(map[string]any)["tools"].([]any)); n != len(mcpTools) {
		t.Errorf("tools/list returned %d", n)
	}
	if r[2]["result"].(map[string]any)["isError"] != nil {
		t.Errorf("save failed: %v", r[2])
	}
	if r[3]["result"].(map[string]any)["isError"] != true {
		t.Errorf("bad remove should be a tool error: %v", r[3])
	}
	if r[4]["error"] == nil {
		t.Error("unknown method should error")
	}
}
