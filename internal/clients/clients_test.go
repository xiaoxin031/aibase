package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookRecognition(t *testing.T) {
	for _, s := range []string{`amem hook`, `'/some path/amem' hook`, `"C:\Program Files\amem.exe" hook stop --agent codex`, `C:\bin\amem.exe hook`, `/bin/amem hook`} {
		if !isHook(s, "amem") {
			t.Errorf("not recognized: %s", s)
		}
	}
	for _, s := range []string{`notamem hook`, `/amem/other hook`, `echo amem hook`, `amem hookish`, `amem --flag hook`, `"/bin/amem hook`, `amemhook`, `amem; hook`, `amem "hook"`, `aibase hook`, `/bin/AMEM hook`, `amem hook && echo keep`, `amem hook; echo keep`} {
		if isHook(s, "amem") {
			t.Errorf("false positive: %s", s)
		}
	}
}
func TestDryRunAndUninstall(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", filepath.Join(h, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(h, ".codex"))
	t.Setenv("GEMINI_CLI_HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	p := filepath.Join(h, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	original := `{"model":"keep","hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"\"C:\\Program Files\\amem.exe\" hook","timeout":4},{"type":"command","command":"notamem hook"}]}]},"permissions":{"allow":["Bash(other:*)"]}}`
	os.WriteFile(p, []byte(original), 0600)
	out, err := Run([]string{"claude", "--dry-run"}, false)
	if err != nil || !strings.Contains(out, "would") {
		t.Fatalf("%s %v", out, err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != original {
		t.Fatal("dry-run changed settings")
	}
	files, _ := filepath.Glob(p + ".bak-*")
	if len(files) != 0 {
		t.Fatal("dry-run backed up")
	}
	if _, err := os.Stat(filepath.Join(h, ".claude", "commands")); !os.IsNotExist(err) {
		t.Fatal("dry-run created dir")
	}
	if _, err := (manager{}).addHooks(p, "/bin/aibase", "claude", true); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(p)
	if strings.Contains(string(installed), "Program Files") || !strings.Contains(string(installed), "notamem hook") {
		t.Fatal(string(installed))
	}
	out, err = Run([]string{"claude"}, true)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(p)
	if strings.Contains(string(got), "aibase") || !strings.Contains(string(got), "notamem hook") || !strings.Contains(string(got), "keep") || !strings.Contains(string(got), "Bash(other:*)") {
		t.Fatalf("%s: %s", out, got)
	}
	files, _ = filepath.Glob(p + ".bak-*")
	if len(files) != 2 {
		t.Fatalf("backups %d", len(files))
	}
	out, err = Run([]string{"claude"}, true)
	if err != nil || !strings.Contains(out, "unchanged") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestMalformedConfigPreserved(t *testing.T) {
	for _, body := range []string{`null`, `{"hooks":false}`, `{"hooks":{"SessionStart":"unexpected"}}`, `{"permissions":{"allow":"unexpected"}}`} {
		p := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(p, []byte(body), 0600)
		if _, err := addHooks(p, "/bin/aibase", "claude", true); err == nil {
			t.Fatalf("accepted malformed shape %s", body)
		}
		got, _ := os.ReadFile(p)
		if string(got) != body {
			t.Fatal("modified malformed config")
		}
	}
}

func TestPreserveNumbers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"large":9007199254740993}`), 0600)
	if _, err := addHooks(p, "/bin/aibase", "claude", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "9007199254740993") {
		t.Fatal(string(b))
	}
}

func TestSessionStartAgent(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		path := filepath.Join(t.TempDir(), "hooks.json")
		if _, err := addHooks(path, "/bin/aibase", agent, agent == "claude"); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		command, _ := json.Marshal(quoteExe("/bin/aibase") + " hook --agent " + agent)
		if err != nil || !strings.Contains(string(b), `"command": `+string(command)) {
			t.Fatalf("%s %v", b, err)
		}
	}
}
