package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/brief"
	"github.com/xiaoxin031/aibase/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xiaoxin031/aibase/internal/audit"
)

func TestOutputBoundaries(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	t.Setenv("AIBASE_HOME", h)
	os.WriteFile(filepath.Join(h, "USER.md"), []byte("中文 TOKEN=private-value\n"), 0600)
	var out, errout bytes.Buffer
	if code := execute([]string{"inject"}, &out, &errout); code != 0 || strings.Contains(out.String(), "private-value") {
		t.Fatalf("%d %s %s", code, out.String(), errout.String())
	}
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false}}`), 0600)
	out.Reset()
	execute([]string{"inject"}, &out, &errout)
	if !strings.Contains(out.String(), "private-value") {
		t.Fatal("opt out ignored")
	}
	out.Reset()
	errout.Reset()
	execute([]string{"show", "sk-abcdefghijklmnop/invalid"}, &out, &errout)
	if strings.Contains(errout.String(), "sk-abcdefghijklmnop") {
		t.Fatal("error leaked")
	}
}
func TestHookAudit(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	t.Setenv("AIBASE_HOME", h)
	os.WriteFile(filepath.Join(h, "USER.md"), []byte("TOKEN=中文-secret\n"), 0600)
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	input.WriteString(`{}`)
	input.Seek(0, 0)
	old := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = old })
	var out, errout bytes.Buffer
	if execute([]string{"hook", "--agent", "claude"}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "中文-secret") {
		t.Fatal(out.String())
	}
	b, _ := os.ReadFile(filepath.Join(h, "audit.jsonl"))
	var e audit.Entry
	if json.Unmarshal(b, &e) != nil || e.Bytes != out.Len() || e.Redactions != 1 || e.Client != "claude" {
		t.Fatalf("%s output bytes=%d", b, out.Len())
	}
}

type planObserver struct {
	bytes.Buffer
	path     string
	original string
	t        *testing.T
	seen     bool
}

func (w *planObserver) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *planObserver) Write(p []byte) (int, error) {
	if !w.seen {
		b, err := os.ReadFile(w.path)
		if err != nil || string(b) != w.original {
			w.t.Fatal("write occurred before plan output")
		}
		backups, _ := filepath.Glob(w.path + ".bak-*")
		if len(backups) != 0 {
			w.t.Fatal("backup occurred before plan output")
		}
		w.seen = true
	}
	return w.Buffer.Write(p)
}

func TestInstallPlanBeforeApply(t *testing.T) {
	h := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := brief.WaitRefresh(ctx); e != nil {
			t.Error(e)
		}
	})
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", filepath.Join(h, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(h, ".codex"))
	t.Setenv("GEMINI_CLI_HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	path := filepath.Join(h, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	original := `{"password":"ordinary-secret-value","nested":{"token":"another-private-value"}}`
	os.WriteFile(path, []byte(original), 0600)
	var dry, errs bytes.Buffer
	if execute([]string{"install", "claude", "--dry-run"}, &dry, &errs) != 0 {
		t.Fatal(errs.String())
	}
	for _, secret := range []string{"ordinary-secret-value", "another-private-value"} {
		if strings.Contains(dry.String(), secret) {
			t.Fatal("dry-run exposed config")
		}
	}
	for _, step := range []string{"SessionStart:", "hook --agent claude", "Stop:", "Bash(aibase:*)", "/save"} {
		if !strings.Contains(dry.String(), step) {
			t.Fatalf("missing plan step %s: %s", step, dry.String())
		}
	}
	observer := &planObserver{path: path, original: original, t: t}
	if execute([]string{"install", "claude"}, observer, &errs) != 0 {
		t.Fatal(errs.String())
	}
	if !observer.seen {
		t.Fatal("no plan output")
	}
	if strings.Contains(observer.String(), "ordinary-secret-value") {
		t.Fatal("apply exposed config")
	}
	backups, _ := filepath.Glob(path + ".bak-*")
	if len(backups) != 1 {
		t.Fatalf("backup count=%d", len(backups))
	}
	b, _ := os.ReadFile(backups[0])
	if string(b) != original {
		t.Fatal("backup differs")
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "ordinary-secret-value") {
		t.Fatal("unrelated config changed")
	}
}

func TestMigrateCLIWithTmpAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix /tmp alias regression")
	}
	root, err := os.MkdirTemp("/tmp", "aibase-migrate-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "source")
	os.Mkdir(src, 0700)
	os.WriteFile(filepath.Join(src, "USER.md"), []byte("sample"), 0600)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(src, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMEM_HOME", alias)
	for _, dest := range []string{filepath.Join(real, "source"), filepath.Join(real, "source", "nested", "new"), root} {
		t.Setenv("AIBASE_HOME", dest)
		var out, errs bytes.Buffer
		if execute([]string{"migrate", "amem"}, &out, &errs) == 0 {
			t.Fatalf("accepted overlap %s", dest)
		}
	}
	dst := filepath.Join(root, "missing", "dest")
	t.Setenv("AIBASE_HOME", dst)
	var out, errs bytes.Buffer
	if execute([]string{"migrate", "amem"}, &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	b, err := os.ReadFile(filepath.Join(dst, "USER.md"))
	if err != nil || string(b) != "sample" {
		t.Fatalf("copy failed %s %v", b, err)
	}
	if execute([]string{"migrate", "amem"}, &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	outside := filepath.Join(root, "outside")
	os.Mkdir(outside, 0700)
	testutil.Symlink(t, outside, filepath.Join(src, "link"))
	if execute([]string{"migrate", "amem"}, &out, &errs) == 0 {
		t.Fatal("accepted source symlink")
	}
	os.Remove(filepath.Join(src, "link"))
	os.Mkdir(filepath.Join(src, "sub"), 0700)
	os.WriteFile(filepath.Join(src, "sub", "new"), []byte("data"), 0600)
	testutil.Symlink(t, outside, filepath.Join(dst, "sub"))
	if execute([]string{"migrate", "amem"}, &out, &errs) == 0 {
		t.Fatal("accepted target escape")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatal("escaped target tree")
	}
}
