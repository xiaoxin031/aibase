package main

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
)

func TestAllJSONCommands(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME", "XDG_CONFIG_HOME", "CODEX_HOME", "GEMINI_CLI_HOME", "AMEM_HOME"} {
		t.Setenv(k, h)
	}
	for _, args := range [][]string{{"help"}, {"help", "save"}, {"ls"}, {"context"}, {"doctor"}, {"audit"}, {"install", "cursor", "--dry-run"}, {"install", "--print-agents-md"}, {"skill", "new", "--draft", "demo"}, {"skill", "show", "--draft", "demo"}, {"skill", "check"}, {"nonsense"}, {"inspect", "host", "--level", "wrong"}, {"skill", "show", "../escape"}} {
		var out, errs bytes.Buffer
		execute(append(args, "--json"), &out, &errs)
		if !json.Valid(out.Bytes()) || errs.Len() != 0 {
			t.Fatalf("%v: %s / %s", args, out.String(), errs.String())
		}
	}
}
func TestNewSessionProtocols(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME"} {
		t.Setenv(k, h)
	}
	for _, agent := range []string{"cursor", "gemini"} {
		f, e := os.CreateTemp(h, "input")
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(map[string]any{"workspace_roots": []string{filepath.Join(h, "workspace")}})
		f.Write(b)
		f.Seek(0, 0)
		old := os.Stdin
		os.Stdin = f
		out, e := sessionHook([]string{"--agent", agent})
		os.Stdin = old
		f.Close()
		if e != nil || !json.Valid([]byte(out)) {
			t.Fatalf("%s %v", out, e)
		}
		if agent == "cursor" && (!strings.Contains(out, "additional_context") || !strings.Contains(out, "workspace")) {
			t.Fatal(out)
		}
	}
}

func TestRefreshArguments(t *testing.T) {
	for _, args := range [][]string{{"--level", "runtime"}, {"--level"}, {"--level="}, {"unexpected"}} {
		if _, e := refreshCommand(args); e == nil {
			t.Fatal(args)
		}
	}
}

func TestProjectHumanMissingOmitted(t *testing.T) {
	out := `{"root":{"value":"/project","status":"ok"},"package.json":{"value":null,"status":"missing"},"git.commits":{"value":"no commits yet","status":"ok"}}`
	got := humanFacts("inspect", out)
	if strings.Contains(got, "package.json") || !strings.Contains(got, "git.commits: no commits yet") || !strings.Contains(got, "root: /project") {
		t.Fatal(got)
	}
}
