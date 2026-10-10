package memory

import (
	"github.com/xiaoxin031/aibase/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func realTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestMigrate(t *testing.T) {
	root := realTemp(t)
	src := filepath.Join(root, "old")
	dst := filepath.Join(root, "new")
	os.MkdirAll(filepath.Join(src, "notes"), 0700)
	os.WriteFile(filepath.Join(src, "notes", "one.md"), []byte("原文"), 0600)
	os.WriteFile(filepath.Join(src, "USER.md"), []byte("old"), 0600)
	os.MkdirAll(dst, 0700)
	os.WriteFile(filepath.Join(dst, "USER.md"), []byte("existing"), 0600)
	t.Setenv("AMEM_HOME", src)
	t.Setenv("AIBASE_HOME", dst)
	out, err := Migrate()
	if err != nil || !strings.Contains(out, "copied notes") || !strings.Contains(out, "skipped USER.md") {
		t.Fatalf("%s %v", out, err)
	}
	for _, p := range []string{filepath.Join(src, "notes", "one.md"), filepath.Join(dst, "notes", "one.md")} {
		b, _ := os.ReadFile(p)
		if string(b) != "原文" {
			t.Fatal(p)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dst, "USER.md"))
	if string(b) != "existing" {
		t.Fatal("overwritten")
	}
	out, err = Migrate()
	if err != nil || strings.Contains(out, "copied") {
		t.Fatalf("not idempotent: %s %v", out, err)
	}
}
func TestMigrationRejectsEscape(t *testing.T) {
	root := realTemp(t)
	src := filepath.Join(root, "source")
	os.Mkdir(src, 0700)
	t.Setenv("AMEM_HOME", src)
	for _, dst := range []string{src, filepath.Join(src, "nested"), root} {
		t.Setenv("AIBASE_HOME", dst)
		if _, err := Migrate(); err == nil {
			t.Fatalf("accepted overlap %s", dst)
		}
	}
	dst := filepath.Join(root, "dest")
	t.Setenv("AIBASE_HOME", dst)
	outside := filepath.Join(root, "outside")
	os.Mkdir(outside, 0700)
	os.WriteFile(filepath.Join(outside, "secret"), []byte("never copy"), 0600)
	testutil.Symlink(t, outside, filepath.Join(src, "link"))
	if _, err := Migrate(); err == nil {
		t.Fatal("source symlink accepted")
	}
	os.Remove(filepath.Join(src, "link"))
	os.MkdirAll(dst, 0700)
	testutil.Symlink(t, outside, filepath.Join(dst, "sub"))
	os.Mkdir(filepath.Join(src, "sub"), 0700)
	os.WriteFile(filepath.Join(src, "sub", "new"), []byte("data"), 0600)
	if _, err := Migrate(); err == nil {
		t.Fatal("target symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatal("escaped destination")
	}
}

func TestProtectedFileInput(t *testing.T) {
	setup(t)
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("TOKEN=do-not-read"), 0600)
	if _, err := input([]string{p}); err == nil {
		t.Fatal("read env")
	}
	if _, err := importNotes([]string{p}); err == nil {
		t.Fatal("imported env")
	}
}

func TestMigrationDirectoryConflict(t *testing.T) {
	root := realTemp(t)
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	os.MkdirAll(filepath.Join(src, "notes"), 0700)
	os.MkdirAll(dst, 0700)
	os.WriteFile(filepath.Join(src, "notes", "one.md"), []byte("source"), 0600)
	os.WriteFile(filepath.Join(dst, "notes"), []byte("keep"), 0600)
	t.Setenv("AMEM_HOME", src)
	t.Setenv("AIBASE_HOME", dst)
	out, err := Migrate()
	if err != nil || !strings.Contains(out, "skipped notes") {
		t.Fatalf("%s %v", out, err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "notes"))
	if string(b) != "keep" {
		t.Fatal("changed conflict")
	}
}

func TestMigrationCaseAlias(t *testing.T) {
	root := realTemp(t)
	src := filepath.Join(root, "source")
	os.Mkdir(src, 0700)
	alias := filepath.Join(root, "SOURCE")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	t.Setenv("AMEM_HOME", src)
	t.Setenv("AIBASE_HOME", filepath.Join(alias, "nested"))
	if _, err := Migrate(); err == nil {
		t.Fatal("case alias bypassed overlap check")
	}
	if _, err := os.Stat(filepath.Join(src, "nested")); !os.IsNotExist(err) {
		t.Fatal("created overlapping destination")
	}
}
