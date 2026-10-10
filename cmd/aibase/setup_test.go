package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "AIBASE_HOME": filepath.Join(h, "data"), "AMEM_HOME": filepath.Join(h, "amem"), "CODEX_HOME": filepath.Join(h, ".codex"), "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "GEMINI_CLI_HOME": h, "PATH": ""} {
		t.Setenv(k, v)
	}
	return h
}
func TestSetupDryRunAndFailureIsolation(t *testing.T) {
	h := setupHome(t)
	os.MkdirAll(filepath.Join(h, "amem"), 0700)
	os.WriteFile(filepath.Join(h, "amem", "user.md"), []byte("fixture memory"), 0600)
	os.MkdirAll(filepath.Join(h, ".claude"), 0700)
	os.WriteFile(filepath.Join(h, ".claude", "settings.json"), []byte("broken"), 0600)
	for _, dry := range []bool{true, false} {
		args := []string{"setup", "--json"}
		if dry {
			args = append(args, "--dry-run")
		}
		var out, errs bytes.Buffer
		code := execute(args, &out, &errs)
		if code != 1 || !json.Valid(out.Bytes()) || errs.Len() != 0 {
			t.Fatal(code, out.String(), errs.String())
		}
		if !strings.Contains(out.String(), "doctor") || !strings.Contains(out.String(), "refresh") {
			t.Fatal("missing partial report", out.String())
		}
		if dry {
			if _, e := os.Stat(filepath.Join(h, "data")); !os.IsNotExist(e) {
				t.Fatal("dry run wrote data")
			}
			if _, e := os.Stat(filepath.Join(h, ".claude", "commands")); !os.IsNotExist(e) {
				t.Fatal("dry run wrote config")
			}
		} else {
			for _, p := range []string{"data/user.md", "data/cache", ".claude/commands/save.md", "data/client-state/claude.json"} {
				if _, e := os.Stat(filepath.Join(h, p)); e != nil {
					t.Fatal(p, e)
				}
			}
		}
	}
	before, _ := os.ReadFile(filepath.Join(h, "data/user.md"))
	setupCommand(nil)
	after, _ := os.ReadFile(filepath.Join(h, "data/user.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("migration not idempotent")
	}
}
func TestHelpAndInstallRefresh(t *testing.T) {
	h := setupHome(t)
	var out, errs bytes.Buffer
	if execute(nil, &out, &errs) != 0 || !strings.Contains(out.String(), "aibase — the local context layer for every AI coding agent") || !strings.Contains(out.String(), "aibase setup") {
		t.Fatal(out.String())
	}
	for _, group := range []string{"Getting started", "Context", "Memory", "Skills", "Integration", "Maintenance"} {
		if !strings.Contains(out.String(), group) {
			t.Fatal(group)
		}
	}
	out.Reset()
	if execute([]string{"install", "claude", "--json"}, &out, &errs) != 0 {
		t.Fatal(out.String(), errs.String())
	}
	if _, e := os.Stat(filepath.Join(h, "data/cache")); e != nil {
		t.Fatal(e)
	}
}
