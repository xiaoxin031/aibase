package clients

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary doubles as a fake `codex app-server` when AIBASE_FAKE_CODEX
// names the hooks file it should report; otherwise no test reaches a real Codex.
func TestMain(m *testing.M) {
	if hooks := os.Getenv("AIBASE_FAKE_CODEX"); hooks != "" {
		fakeCodex(hooks)
		os.Exit(0)
	}
	codexBin = "aibase-test-no-codex"
	os.Exit(m.Run())
}

func fakeCodex(hooks string) {
	status := os.Getenv("AIBASE_FAKE_CODEX_STATUS")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID     *int           `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.Unmarshal(sc.Bytes(), &req)
		if req.ID == nil {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "hooks/list":
			list := []map[string]any{}
			for _, e := range []string{"session_start", "stop"} {
				list = append(list, map[string]any{"key": hooks + ":" + e + ":0:0", "command": "/bin/aibase hook --agent codex", "sourcePath": hooks, "currentHash": "sha256:" + e, "trustStatus": status, "enabled": true})
			}
			result = map[string]any{"data": []any{map[string]any{"hooks": list}}}
		case "config/batchWrite":
			b, _ := json.Marshal(req.Params)
			os.WriteFile(os.Getenv("AIBASE_FAKE_CODEX_LOG"), b, 0600)
			status = "trusted"
		}
		// A notification first proves unrelated server messages are skipped.
		os.Stdout.WriteString(`{"jsonrpc":"2.0","method":"configWarning","params":{}}` + "\n")
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
		os.Stdout.Write(append(b, '\n'))
	}
}

func fakeCodexEnv(t *testing.T, status string) (hooks, log string) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("CODEX_HOME", filepath.Join(h, ".codex"))
	hooks = filepath.Join(h, ".codex", "hooks.json")
	log = filepath.Join(h, "batch.json")
	os.MkdirAll(filepath.Dir(hooks), 0700)
	os.WriteFile(hooks, []byte("{}"), 0600)
	os.WriteFile(filepath.Join(h, ".codex", "config.toml"), []byte("model = \"keep\"\n"), 0600)
	exe, _ := os.Executable()
	old := codexBin
	codexBin = exe
	t.Cleanup(func() { codexBin = old })
	t.Setenv("AIBASE_FAKE_CODEX", hooks)
	t.Setenv("AIBASE_FAKE_CODEX_STATUS", status)
	t.Setenv("AIBASE_FAKE_CODEX_LOG", log)
	return hooks, log
}

func TestTrustCodexHooks(t *testing.T) {
	hooks, log := fakeCodexEnv(t, "modified")
	if got := CodexHookTrust(); got != "untrusted" {
		t.Fatalf("before: %s", got)
	}
	msg := manager{}.trustCodexHooks(hooks)
	if !strings.HasPrefix(msg, "updated Codex hook trust (2 hooks") {
		t.Fatal(msg)
	}
	b, _ := os.ReadFile(log)
	var p struct {
		Edits []struct {
			KeyPath string                    `json:"keyPath"`
			Value   map[string]map[string]any `json:"value"`
		} `json:"edits"`
	}
	if json.Unmarshal(b, &p) != nil || len(p.Edits) != 1 || p.Edits[0].KeyPath != "hooks.state" {
		t.Fatalf("batch write: %s", b)
	}
	e := p.Edits[0].Value[hooks+":stop:0:0"]
	if e["trusted_hash"] != "sha256:stop" || e["enabled"] != true {
		t.Fatalf("stop edit: %v", e)
	}
	baks, _ := filepath.Glob(filepath.Join(filepath.Dir(hooks), "config.toml.bak-aibase-*"))
	if len(baks) != 1 {
		t.Fatalf("config.toml backups: %v", baks)
	}
}

func TestTrustCodexHooksAlreadyTrusted(t *testing.T) {
	hooks, log := fakeCodexEnv(t, "trusted")
	if msg := (manager{}).trustCodexHooks(hooks); msg != "unchanged Codex hook trust" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("trusted hooks must not be rewritten")
	}
	if got := CodexHookTrust(); got != "trusted" {
		t.Fatal(got)
	}
}

func TestTrustCodexHooksWithoutCodex(t *testing.T) {
	msg := manager{}.trustCodexHooks(filepath.Join(t.TempDir(), "hooks.json"))
	if !strings.HasPrefix(msg, "skipped Codex hook trust (codex not on PATH)") || !strings.Contains(msg, "/hooks") {
		t.Fatal(msg)
	}
	if msg := (manager{dryRun: true}).trustCodexHooks("x"); !strings.HasPrefix(msg, "skipped ") {
		t.Fatal(msg)
	}
	if got := CodexHookTrust(); got != "unknown" {
		t.Fatal(got)
	}
}
