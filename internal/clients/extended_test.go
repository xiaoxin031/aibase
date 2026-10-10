package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedClients(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", filepath.Join(h, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("CODEX_HOME", filepath.Join(h, "codex"))
	t.Setenv("GEMINI_CLI_HOME", h)
	return h
}
func TestExtendedInstallAndRestore(t *testing.T) {
	for _, target := range []string{"gemini", "cursor", "opencode", "codex"} {
		t.Run(target, func(t *testing.T) {
			isolatedClients(t)
			if _, e := Run([]string{target, "--dry-run"}, false); e != nil {
				t.Fatal(e)
			}
			if _, e := os.Stat(clientDir(target)); !os.IsNotExist(e) {
				t.Fatal("dry run wrote client")
			}
			if _, e := Run([]string{target}, false); e != nil {
				t.Fatal(e)
			}
			paths := managedPaths(target)
			p := paths[0]
			b, _ := os.ReadFile(p)
			r, e := decodeRoot(b)
			if e != nil {
				t.Fatal(e)
			}
			r["user_added"] = "preserve"
			b, _ = json.Marshal(r)
			os.WriteFile(p, b, 0600)
			if _, e = Run([]string{target}, true); e != nil {
				t.Fatal(e)
			}
			b, _ = os.ReadFile(p)
			if !strings.Contains(string(b), "preserve") || strings.Contains(string(b), "hook --agent") || strings.Contains(string(b), `"aibase"`) {
				t.Fatal(string(b))
			}
		})
	}
}
func TestCursorShapeAndAlias(t *testing.T) {
	isolatedClients(t)
	if _, e := Run([]string{"cursor-agent"}, false); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(clientDir("cursor"), "hooks.json"))
	r, e := decodeRoot(b)
	if e != nil {
		t.Fatal(e)
	}
	hooks := r["hooks"].(map[string]any)
	entry := hooks["sessionStart"].([]any)[0].(map[string]any)
	if entry["command"] == nil || entry["hooks"] != nil {
		t.Fatal(entry)
	}
	b, _ = os.ReadFile(filepath.Join(clientDir("cursor"), "mcp.json"))
	if !strings.Contains(string(b), "mcpServers") {
		t.Fatal(string(b))
	}
}
func TestPrintAndConflicts(t *testing.T) {
	h := isolatedClients(t)
	if _, e := Run([]string{"--print-agents-md"}, false); e != nil {
		t.Fatal(e)
	}
	if ds, _ := os.ReadDir(h); len(ds) != 0 {
		t.Fatal("print wrote files")
	}
	p := filepath.Join(clientDir("cursor"), "mcp.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	body := `{"mcpServers":{"aibase":{"command":"user-server"}}}`
	os.WriteFile(p, []byte(body), 0600)
	if _, e := Run([]string{"cursor"}, false); e == nil {
		t.Fatal("conflict overwritten")
	}
	if _, e := os.Stat(filepath.Join(clientDir("cursor"), "hooks.json")); e != nil {
		t.Fatal("independent hooks step did not succeed")
	}
	for _, b := range []string{`{/*comment*/}`, `{"hooks":{},"hooks":{}}`} {
		os.WriteFile(p, []byte(b), 0600)
		if _, e := Run([]string{"cursor"}, false); e == nil {
			t.Fatal("unsupported JSON accepted")
		}
	}
}
func TestUninstallPreservesAddedHook(t *testing.T) {
	isolatedClients(t)
	Run([]string{"gemini"}, false)
	p := filepath.Join(clientDir("gemini"), "settings.json")
	b, _ := os.ReadFile(p)
	r, _ := decodeRoot(b)
	hooks := r["hooks"].(map[string]any)
	g := hooks["SessionStart"].([]any)[0].(map[string]any)
	g["hooks"] = append(g["hooks"].([]any), map[string]any{"type": "command", "command": "user-custom"})
	b, _ = json.Marshal(r)
	os.WriteFile(p, b, 0600)
	if _, e := Run([]string{"gemini"}, true); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "hook --agent") || !strings.Contains(string(b), "user-custom") {
		t.Fatal(string(b))
	}
}

func TestReinstallRetainsUserChanges(t *testing.T) {
	isolatedClients(t)
	if _, e := Run([]string{"cursor"}, false); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(clientDir("cursor"), "mcp.json")
	b, _ := os.ReadFile(p)
	r, _ := decodeRoot(b)
	r["added_after_install"] = true
	b, _ = json.Marshal(r)
	os.WriteFile(p, b, 0600)
	if _, e := Run([]string{"cursor"}, false); e != nil {
		t.Fatal(e)
	}
	if _, e := Run([]string{"cursor"}, true); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(p)
	if !strings.Contains(string(b), "added_after_install") || strings.Contains(string(b), `"aibase"`) {
		t.Fatal(string(b))
	}
}

func TestDisabledAndWrongEventDetection(t *testing.T) {
	good := map[string]any{"type": "command", "command": "aibase hook stop --agent claude"}
	if hasEventHook(good, false, "claude") {
		t.Fatal("stop command accepted as start")
	}
	if !hasEventHook(good, true, "claude") {
		t.Fatal("valid stop rejected")
	}
	good["disabled"] = true
	if hasEventHook(good, true, "claude") {
		t.Fatal("disabled hook accepted")
	}
	for _, command := range []string{"not-aibase", "aibase"} {
		r := map[string]any{"mcpServers": map[string]any{"aibase": map[string]any{"command": command, "args": []any{"mcp"}, "disabled": true}}}
		if hasMCP(r, "cursor") {
			t.Fatal("disabled or incorrect MCP accepted")
		}
	}
}
func TestJSONCPreventsAmbiguousInstall(t *testing.T) {
	isolatedClients(t)
	p := filepath.Join(clientDir("opencode"), "opencode.jsonc")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("{/* sentinel */}"), 0600)
	if _, e := Run([]string{"opencode"}, false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(clientDir("opencode"), "opencode.json")); !os.IsNotExist(e) {
		t.Fatal("created competing config")
	}
}
