package redact

import (
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRules(t *testing.T) {
	for _, s := range []string{
		"中文 sk-abcdefghijklmnop 中文", "sk-ant-abcdefghijklmnop", "AKIAABCDEFGHIJKLMNOP", "ASIAABCDEFGHIJKLMNOP", "ghp_abcdefghijklmnop", "github_pat_abcdefghijklmnop", "glpat-abcdefghijklmnop", "xoxb-1234567890-abcdefgh", "xoxa-1234567890", "xoxp-1234567890", "AIzaabcdefghijklmnopqrstuvwxyz012345678", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl", "-----BEGIN RSA PRIVATE KEY-----\n秘密\n-----END RSA PRIVATE KEY-----", "https://user:中文密码@example.com/path", "Bearer abc.def-123", "SECRET=中文秘密", "MY_TOKEN='with spaces'", `password="a b"`, "db_passwd=pass", "api_key=abc", "CREDENTIAL=abcd", "PRIVATE_KEY=hi",
	} {
		out, n := Text(s)
		if n != 1 || out == s || !strings.Contains(out, "[REDACTED]") {
			t.Errorf("rule failed: %q => %q (%d)", s, out, n)
		}
		if _, n := Text(out); n != 0 {
			t.Errorf("not idempotent: %q", out)
		}
	}
	for _, s := range []string{"中文普通内容", "HASH=abcdef0123456789", "550e8400-e29b-41d4-a716-446655440000", "/Users/user/token/project", "https://example.com/path", "NAME=hello", "TOKEN", "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----"} {
		out, n := Text(s)
		if out != s || n != 0 {
			t.Errorf("false positive: %q => %q", s, out)
		}
	}
	out, n := Text("SECRET=sk-abcdefghijklmnop TOKEN=中文")
	if n != 2 || strings.Count(out, "[REDACTED]") != 2 {
		t.Fatalf("overlap count: %s %d", out, n)
	}
}
func TestJSON(t *testing.T) {
	in := []byte(`{"sk-abcdefghijklmnop":{"value":"TOKEN=中文","n":9007199254740993},"array":["Bearer abcdef",true,null]}`)
	out, n, err := JSON(in)
	if err != nil || n != 2 || !json.Valid(out) {
		t.Fatalf("%s %d %v", out, n, err)
	}
	if !strings.Contains(string(out), `"sk-abcdefghijklmnop"`) || !strings.Contains(string(out), "9007199254740993") {
		t.Fatal(string(out))
	}
}
func TestScanExclusions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".ssh/key", ".aws/credentials", ".env", ".env.local", "Library/Keychains/login.keychain", "Library/Application Support/Google/Chrome/Default/password", "ok.txt"} {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte("line one\nTOKEN=hidden\n"), 0600)
	}
	t.Run("symlink", func(t *testing.T) { testutil.Symlink(t, filepath.Join(root, "ok.txt"), filepath.Join(root, "link")) })
	out, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hidden") || strings.Count(out, ":2:assignment") != 1 || !strings.Contains(out, "ok.txt:2:assignment") {
		t.Fatal(out)
	}
	out, err = Scan(filepath.Join(root, ".env"))
	if err != nil || strings.Contains(out, "assignment") || !strings.Contains(out, "excluded") {
		t.Fatalf("%s %v", out, err)
	}
}
func TestMemoryConfig(t *testing.T) {
	h := t.TempDir()
	if !MemoryEnabled(h) {
		t.Fatal("default")
	}
	for _, s := range []string{`{}`, `{"redact":{"memory":true}}`, `bad`} {
		os.WriteFile(filepath.Join(h, "config.json"), []byte(s), 0600)
		if !MemoryEnabled(h) {
			t.Fatal(s)
		}
	}
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false}}`), 0600)
	if MemoryEnabled(h) {
		t.Fatal("false ignored")
	}
}

func TestSensitiveAlias(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, ".ssh")
	os.Mkdir(secret, 0700)
	os.WriteFile(filepath.Join(secret, "key"), []byte("TOKEN=never-read"), 0600)
	alias := filepath.Join(root, "alias")
	testutil.Symlink(t, secret, alias)
	out, err := Scan(filepath.Join(alias, "key"))
	if err != nil || strings.Contains(out, "assignment") || !strings.Contains(out, "excluded") {
		t.Fatalf("%s %v", out, err)
	}
	if CheckPath(filepath.Join(alias, "key")) == nil {
		t.Fatal("explicit sensitive alias allowed")
	}
}

func TestMarkerDoesNotBypassRedaction(t *testing.T) {
	out, n := Text("TOKEN=prefix[REDACTED]still-secret")
	if n != 1 || strings.Contains(out, "still-secret") {
		t.Fatalf("%s %d", out, n)
	}
}

func TestOverlappingPEMAndMarkers(t *testing.T) {
	for _, input := range []string{
		"PRIVATE_KEY=-----BEGIN RSA PRIVATE KEY-----\nsecret-tail\n-----END RSA PRIVATE KEY-----",
		"PRIVATE_KEY=\"-----BEGIN PRIVATE KEY-----\nsecret-tail\n-----END PRIVATE KEY-----",
		"TOKEN=abc[REDACTED]secret", "TOKEN='abc[REDACTED]secret'",
	} {
		out, n := Text(input)
		if n != 1 || out != "PRIVATE_KEY=[REDACTED]" && out != "TOKEN=[REDACTED]" {
			t.Fatalf("%q => %q (%d)", input, out, n)
		}
		twice, n := Text(out)
		if twice != out || n != 0 {
			t.Fatalf("not idempotent: %q", twice)
		}
	}
	for _, input := range []string{"TOKEN=[REDACTED]", "NAME=abc[REDACTED]public", "[REDACTED] ordinary text"} {
		out, n := Text(input)
		if out != input || n != 0 {
			t.Fatalf("false positive: %q", out)
		}
	}
}

func TestOversizedConfigFailsClosed(t *testing.T) {
	h := t.TempDir()
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false},"padding":"`+strings.Repeat("x", 70<<10)+`"}`), 0600)
	if !MemoryEnabled(h) {
		t.Fatal("oversized config disabled redaction")
	}
}

func TestBrowserPathsExcluded(t *testing.T) {
	for _, p := range []string{"/home/example/.config/google-chrome/Default", "/home/example/.config/chromium/Default", "C:/Users/example/AppData/Local/Microsoft/Edge/User Data", "C:/Users/example/AppData/Local/Google/Chrome/User Data", "C:/Users/example/AppData/Local/Microsoft/Credentials"} {
		if !Forbidden(p) {
			t.Fatal(p)
		}
	}
}

func TestScanReadBudgetAndWindowsSeparators(t *testing.T) {
	if !Forbidden(`C:\Users\demo\.ssh\id_ed25519`) {
		t.Fatal("Windows sensitive path missed")
	}
	h := t.TempDir()
	os.WriteFile(filepath.Join(h, "oversized.txt"), []byte(strings.Repeat("x", (1<<20)+1)), 0600)
	s, e := Scan(h)
	if e != nil || !strings.Contains(s, "not scanned") {
		t.Fatal(s, e)
	}
}
