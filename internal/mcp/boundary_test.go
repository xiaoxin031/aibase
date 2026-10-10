package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/brief"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaoxin031/aibase/internal/audit"
)

func TestRedactionAuditAndOptOut(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	t.Setenv("AIBASE_HOME", h)
	os.WriteFile(filepath.Join(h, "USER.md"), []byte("TOKEN=private-value"), 0600)
	for _, optOut := range []bool{false, true} {
		if optOut {
			os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false}}`), 0600)
		}
		var out bytes.Buffer
		if err := Serve(strings.NewReader(`{"jsonrpc":"2.0","id":9007199254740993,"method":"initialize","params":{"protocolVersion":"Bearer protocol-secret"}}`), &out); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "protocol-secret") || !strings.Contains(out.String(), "9007199254740993") {
			t.Fatal(out.String())
		}
		if strings.Contains(out.String(), "private-value") != optOut {
			t.Fatal(out.String())
		}
		b, _ := os.ReadFile(filepath.Join(h, "audit.jsonl"))
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		var e audit.Entry
		json.Unmarshal([]byte(lines[len(lines)-1]), &e)
		want := 2
		if optOut {
			want = 1
		}
		if e.Bytes != out.Len() || e.Redactions != want {
			t.Fatalf("%+v output len=%d", e, out.Len())
		}
	}
	var out bytes.Buffer
	Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sk-abcdefghijklmnop"}}`), &out)
	if strings.Contains(out.String(), "sk-abcdefghijklmnop") {
		t.Fatal(out.String())
	}
	b, _ := os.ReadFile(filepath.Join(h, "audit.jsonl"))
	if strings.Contains(string(b), "sk-abcdefghijklmnop") || strings.Contains(string(b), "private-value") {
		t.Fatal("audit secret")
	}
}

func TestSecretLookingCorrelationID(t *testing.T) {
	t.Setenv("AIBASE_HOME", t.TempDir())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	for _, method := range []string{"ping", "sk-abcdefghijklmnop"} {
		for _, id := range []string{"sk-abcdefghijklmnop", "TOKEN=abc[REDACTED]secret", "Bearer abcdef"} {
			request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method})
			var out bytes.Buffer
			if err := Serve(bytes.NewReader(request), &out); err != nil {
				t.Fatal(err)
			}
			var response struct {
				ID    string `json:"id"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.ID != id {
				t.Fatalf("%s: %v", out.String(), err)
			}
			if strings.Contains(response.Error.Message, "sk-abcdefghijklmnop") {
				t.Fatal("error not redacted")
			}
		}
	}
}

func TestFactToolsAndSchema(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, h)
	}
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false}}`), 0600)
	for _, raw := range []string{`{"name":"host_inspect","arguments":{"level":"bad"}}`, `{"name":"skill_show","arguments":{}}`, `{"name":"context","arguments":{"cwd":null}}`, `{"name":"skills_list","arguments":{"execute":"yes"}}`} {
		var out bytes.Buffer
		Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+raw+`}`), &out)
		if !strings.Contains(out.String(), `"isError":true`) {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), &out)
	for _, name := range []string{"context", "host_inspect", "project_inspect", "projects_find", "skills_list", "skill_show", "doctor"} {
		if !strings.Contains(out.String(), `"name":"`+name+`"`) {
			t.Fatal(name)
		}
	}
	if strings.Count(out.String(), `"readOnlyHint":true`) != 7 {
		t.Fatal(out.String())
	}
}

func TestPausedInitializationHasNoInjection(t *testing.T) {
	h := t.TempDir()
	t.Setenv("AIBASE_HOME", h)
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"inject":false}`), 0600)
	var out bytes.Buffer
	if e := Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`), &out); e != nil {
		t.Fatal(e)
	}
	var response struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if e := json.Unmarshal(out.Bytes(), &response); e != nil || response.Result.Instructions != "" {
		t.Fatal(out.String(), e)
	}
	entries, e := audit.Read(h, 10)
	if e != nil || strings.Contains(entries, `"action":"SessionStart"`) {
		t.Fatal(entries, e)
	}
}
