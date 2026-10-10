package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putClient(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func readClient(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestLegacyAndThirdPartySave(t *testing.T) {
	for _, old := range []string{"Saving to amem\nlegacy", "---\ndescription: Save to amem\n---\nbody", "---\r\ndescription: >\r\n  Save session in amem\r\n---\r\n", "third party"} {
		t.Run(old, func(t *testing.T) {
			isolatedClients(t)
			p := filepath.Join(clientDir("claude"), "commands/save.md")
			putClient(t, p, old)
			out, e := Run([]string{"claude"}, false)
			if e != nil {
				t.Fatal(e)
			}
			if old == "third party" {
				if readClient(t, p) != old || !strings.Contains(out, "skipped") {
					t.Fatal(out)
				}
			} else {
				if !strings.Contains(readClient(t, p), "Saving to aibase") {
					t.Fatal(out)
				}
				backups, _ := filepath.Glob(p + ".bak-aibase-*")
				if len(backups) != 1 || readClient(t, backups[0]) != old {
					t.Fatal("backup")
				}
			}
			if _, e = Run([]string{"claude"}, false); e != nil {
				t.Fatal(e)
			}
			if _, e = Run([]string{"claude"}, true); e != nil {
				t.Fatal(e)
			}
			if readClient(t, p) != old {
				t.Fatal("not restored")
			}
		})
	}
}
func TestFailuresIndependentAndOwned(t *testing.T) {
	isolatedClients(t)
	putClient(t, filepath.Join(clientDir("claude"), "settings.json"), "invalid")
	putClient(t, filepath.Join(clientDir("cursor"), "hooks.json"), "invalid")
	out, e := Run(nil, false)
	if e == nil || !strings.Contains(out, "failed") {
		t.Fatal(out, e)
	}
	for _, p := range []string{filepath.Join(clientDir("claude"), "commands/save.md"), filepath.Join(clientDir("cursor"), "mcp.json")} {
		if _, e := os.Stat(p); e != nil {
			t.Fatal(e)
		}
	}
	for _, target := range []string{"claude", "cursor"} {
		o, e := loadOwnership(target)
		if e != nil || len(o.Files) != 1 {
			t.Fatal(o, e)
		}
		if _, e = Run([]string{target}, true); e != nil {
			t.Fatal(e)
		}
		if _, e = os.Stat(o.Files[0].Path); !os.IsNotExist(e) {
			t.Fatal("partial success not removed")
		}
	}
}
func TestJSONCFormattingAndRestore(t *testing.T) {
	cases := []string{
		"{ // hello\n \"url\": \"https://example.test/a/*b*/\",\n}",
		"{/*root*/\"mcp\":{/*m*/\"other\":{\"x\":[1,2,],},},\"nested\":{\"a\":true,},}",
		"{\"mcp\":{/*empty*/}, /*tail*/}",
	}
	for _, ext := range []string{".json", ".jsonc"} {
		for _, original := range cases {
			t.Run(ext+original, func(t *testing.T) {
				isolatedClients(t)
				p := filepath.Join(clientDir("opencode"), "opencode"+ext)
				putClient(t, p, original)
				if _, e := Run([]string{"opencode"}, false); e != nil {
					t.Fatal(e)
				}
				after := readClient(t, p)
				if _, e := parseJSONC(after); e != nil {
					t.Fatal(after, e)
				}
				// All original bytes are preserved around the inserted member.
				for _, comment := range []string{"// hello", "/*root*/", "/*m*/", "/*empty*/", "/*tail*/", "https://example.test/a/*b*/"} {
					if strings.Contains(original, comment) && !strings.Contains(after, comment) {
						t.Fatal("lost comment", comment)
					}
				}
				if _, e := Run([]string{"opencode"}, false); e != nil {
					t.Fatal(e)
				}
				if readClient(t, p) != after {
					t.Fatal("not idempotent")
				}
				found := false
				for _, d := range Detect() {
					if d.Name == "opencode" {
						found = d.Configured
					}
				}
				if !found {
					t.Fatal("not detected")
				}
				if _, e := Run([]string{"opencode"}, true); e != nil {
					t.Fatal(e)
				}
				if readClient(t, p) != original {
					t.Fatal("not byte exact restore")
				}
			})
		}
	}
}
func TestJSONCUnsafePreserved(t *testing.T) {
	for _, body := range []string{`{"x":1,"x":2}`, `{"mcp":{"x":1,"x":2}}`, `{/* bad}`, `{"x":,}`, `{"x":01}`, `{"x":[,]}`} {
		isolatedClients(t)
		p := filepath.Join(clientDir("opencode"), "opencode.jsonc")
		putClient(t, p, body)
		if _, e := Run([]string{"opencode"}, false); e == nil {
			t.Fatal("accepted", body)
		}
		if readClient(t, p) != body {
			t.Fatal("changed invalid")
		}
	}
	isolatedClients(t)
	for _, ext := range []string{"json", "jsonc"} {
		putClient(t, filepath.Join(clientDir("opencode"), "opencode."+ext), "{}")
	}
	out, e := Run([]string{"opencode"}, false)
	if e != nil || !strings.Contains(out, "skipped") {
		t.Fatal(out, e)
	}
}
func TestJSONCOwnCommentsAndUpdates(t *testing.T) {
	s := `{"mcp":{"aibase":{/* own */"type":"local","command":["/old/aibase","mcp"],"enabled":false,}}}`
	out, e := editOpencode(s, "/new/aibase")
	if e != nil || !strings.Contains(out, "/* own */") || !strings.Contains(out, "/new/aibase") {
		t.Fatal(out, e)
	}
	bad := strings.Replace(s, `"/old/aibase",`, `"/old/aibase",/* inside */`, 1)
	if _, e = editOpencode(bad, "/new/aibase"); e == nil {
		t.Fatal("lost inner comment")
	}
}
func TestJSONCUninstallAfterUnrelatedEdit(t *testing.T) {
	isolatedClients(t)
	p := filepath.Join(clientDir("opencode"), "opencode.jsonc")
	putClient(t, p, "{/*keep*/\"mcp\":{}}")
	if _, e := Run([]string{"opencode"}, false); e != nil {
		t.Fatal(e)
	}
	body := readClient(t, p)
	body = strings.Replace(body, "{/*keep*/", "{\"user\":true,/*keep*/", 1)
	putClient(t, p, body)
	if _, e := Run([]string{"opencode"}, false); e != nil {
		t.Fatal(e)
	}
	if _, e := Run([]string{"opencode"}, true); e != nil {
		t.Fatal(e)
	}
	got := readClient(t, p)
	if !strings.Contains(got, "/*keep*/") || !strings.Contains(got, "\"user\":true") || strings.Contains(got, "aibase") {
		t.Fatal(got)
	}
}

func TestJSONCRestoreLastOfSeveralMembers(t *testing.T) {
	after := `{"mcp":{"one":{},"two":{},"aibase":{"command":["aibase","mcp"]}}}`
	current := strings.Replace(after, `"two":{}`, `"two":{"user":true}`, 1)
	out, e := restoreOpencode(nil, after, current)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = parseJSONC(out); e != nil {
		t.Fatal(out, e)
	}
	if strings.Contains(out, "aibase") || !strings.Contains(out, "user") {
		t.Fatal(out)
	}
}

func TestInstallRestoreReinstallCycle(t *testing.T) {
	for _, target := range Targets {
		t.Run(target, func(t *testing.T) {
			isolatedClients(t)
			if target == "opencode" {
				putClient(t, filepath.Join(clientDir(target), "opencode.jsonc"), "{/* original */}")
			}
			for i := 0; i < 2; i++ {
				if _, e := Run([]string{target}, false); e != nil {
					t.Fatal(e)
				}
				if _, e := Run([]string{target}, true); e != nil {
					t.Fatal(e)
				}
				if _, e := Run([]string{target}, true); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
