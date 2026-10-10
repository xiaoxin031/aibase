package skills

import (
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolated(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, h)
	}
	t.Setenv("AIBASE_HOME", filepath.Join(h, "data"))
}
func TestParseFrontmatter(t *testing.T) {
	good := []string{"---\nname: sample\ndescription: plain description\nmetadata:\n  requires: 'go, git'\n---\nbody", "---\r\nname: sample\r\ndescription: >-\r\n  中文多步\r\n  流程\r\nmetadata:\r\n  owner: \"a user\"\r\n---\r\nbody"}
	for _, s := range good {
		v, e := Parse([]byte(s))
		if e != nil || v.Name != "sample" || v.Description == "" {
			t.Fatalf("%+v %v", v, e)
		}
	}
	for _, s := range []string{"name: sample", "description: text", "name: \"\"\ndescription: text"} {
		if _, e := Parse([]byte("---\n" + s + "\n---\n")); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestDraftPromoteAndDependency(t *testing.T) {
	isolated(t)
	p, e := New("example", true)
	if e != nil {
		t.Fatal(e)
	}
	b := []byte("---\nname: example\ndescription: test\nmetadata:\n  requires: aibase-impossible-dependency\n---\n")
	os.WriteFile(p, b, 0600)
	if _, e = Promote("example"); e == nil {
		t.Fatal("missing dependency promoted")
	}
	os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "aibase-impossible-dependency", "''")), 0600)
	os.WriteFile(filepath.Join(filepath.Dir(p), "data.txt"), []byte("fixture"), 0600)
	if _, e = Promote("example"); e != nil {
		t.Fatal(e)
	}
	if _, e = Read(filepath.Join(Root(false), "example", "data.txt")); e != nil {
		t.Fatal(e)
	}
	if _, e = Promote("example"); e == nil {
		t.Fatal("overwrote skill")
	}
}
func TestPathLimitsAndOwnership(t *testing.T) {
	isolated(t)
	for _, name := range []string{"../escape", "UPPER", "nul", "a/b", "a\\b"} {
		if _, e := New(name, false); e == nil {
			t.Fatal(name)
		}
	}
	p, e := New("sample", false)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, make([]byte, MaxSize+1), 0600)
	if _, e = Show("sample", false); e == nil {
		t.Fatal("oversize accepted")
	}
	os.Remove(p)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("---\nname: sample\ndescription: hi\n---\n"), 0600)
	t.Run("symlink", func(t *testing.T) {
		testutil.Symlink(t, outside, p)
		if _, e = Show("sample", false); e == nil {
			t.Fatal("symlink read")
		}
	})
}
func TestSyncPreservesExisting(t *testing.T) {
	isolated(t)
	New("sample", false)
	h, _ := os.UserHomeDir()
	dst := filepath.Join(h, ".claude", "skills", "sample")
	os.MkdirAll(dst, 0700)
	os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("user file"), 0600)
	if _, e := Sync(); e == nil {
		t.Fatal("unowned directory accepted")
	}
	b, _ := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if string(b) != "user file" {
		t.Fatal("modified user file")
	}
}

func TestDirectoryAndCaseLimits(t *testing.T) {
	isolated(t)
	root := Root(false)
	os.MkdirAll(root, 0700)
	os.Mkdir(filepath.Join(root, "Example"), 0700)
	if _, e := New("example", false); e == nil {
		t.Fatal("case collision")
	}
	for i := 0; i < MaxSkills; i++ {
		os.Mkdir(filepath.Join(root, fmt.Sprintf("skill-%d", i)), 0700)
	}
	if _, e := List(false); e == nil {
		t.Fatal("unbounded list")
	}
}

func TestYAMLBlocks(t *testing.T) {
	for _, tc := range []struct{ style, want string }{{"|-", "first\nsecond\n\nthird"}, {"|", "first\nsecond\n\nthird\n"}, {">-", "first second\nthird"}, {">", "first second\nthird\n"}} {
		s, e := Parse([]byte("---\nname: sample\ndescription: " + tc.style + "\n  first\n  second\n\n  third\n\n---\n"))
		if e != nil || s.Description != tc.want {
			t.Fatalf("%s: %q %v", tc.style, s.Description, e)
		}
	}
}

func TestOwnedCopyRefreshAndUserConflict(t *testing.T) {
	isolated(t)
	p, e := New("sample", false)
	if e != nil {
		t.Fatal(e)
	}
	src := filepath.Dir(p)
	h, _ := os.UserHomeDir()
	dst := filepath.Join(h, ".claude", "skills", "sample")
	os.MkdirAll(filepath.Dir(dst), 0700)
	if e = copyTree(src, dst); e != nil {
		t.Fatal(e)
	}
	hash, e := treeHash(dst)
	if e != nil {
		t.Fatal(e)
	}
	record := filepath.Join(cache.Home(), "skill-sync.json")
	if e = saveSyncRecord(record, map[string]string{dst: src, dst + "#sha256": hash}); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(src, "fixture.txt"), []byte("new content"), 0600)
	if _, e = Sync(); e != nil {
		t.Fatal(e)
	}
	b, e := Read(filepath.Join(dst, "fixture.txt"))
	if e != nil || string(b) != "new content" {
		t.Fatalf("%s %v", b, e)
	}
	os.WriteFile(filepath.Join(dst, "fixture.txt"), []byte("user edit"), 0600)
	if _, e = Sync(); e == nil {
		t.Fatal("modified copy overwritten")
	}
	b, _ = Read(filepath.Join(dst, "fixture.txt"))
	if string(b) != "user edit" {
		t.Fatal(string(b))
	}
}

func TestDependencyShellSyntaxRejected(t *testing.T) {
	isolated(t)
	p, e := New("unsafe-dep", false)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("---\nname: unsafe-dep\ndescription: test\nmetadata:\n  requires: 'foo;touch'\n---\n"), 0600)
	if _, e = Show("unsafe-dep", false); e == nil {
		t.Fatal("shell metacharacter accepted")
	}
}

func TestSyncWithoutSymlinkPrivilege(t *testing.T) {
	isolated(t)
	src, err := New("sample", false)
	if err != nil {
		t.Fatal(err)
	}
	denied := func(string, string) error { return os.ErrPermission }
	if _, err = syncSkills(denied, true); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	dst := filepath.Join(home, ".claude", "skills", "sample")
	st, err := os.Lstat(dst)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		t.Fatal(st, err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(src), "extra.txt"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = syncSkills(denied, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "extra.txt"))
	if err != nil || string(b) != "updated" {
		t.Fatal(string(b), err)
	}
	if _, err = syncSkills(denied, true); err != nil {
		t.Fatal("idempotent refresh", err)
	}
}
