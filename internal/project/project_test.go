package project

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/testutil"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type gitRunner struct {
	mu    sync.Mutex
	calls []string
	delay bool
}

func (r *gitRunner) LookPath(s string) (string, error) { return s, nil }
func (r *gitRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	r.mu.Unlock()
	if name != "git" {
		return nil, fmt.Errorf("unexpected execution")
	}
	if r.delay {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := strings.Join(args, " ")
	switch {
	case strings.Contains(s, "branch --show-current"):
		return []byte("main"), nil
	case strings.Contains(s, "remote -v"):
		return []byte("origin https://user:secret@example.com/repo?token=hidden (fetch)\nother git@host:repo (push)"), nil
	case strings.Contains(s, "rev-list"):
		return []byte("2 3"), nil
	case strings.Contains(s, "--format=%cI"):
		return []byte("2026-01-02T03:04:05+00:00"), nil
	case strings.Contains(s, "status"):
		return []byte(" M file\x00R  new\x00old\x00?? untracked\x00"), nil
	default:
		return []byte("fix TOKEN=hidden"), nil
	}
}
func write(t *testing.T, root, name, s string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func setup(t *testing.T) (string, *Inspector, *gitRunner) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", filepath.Join(h, ".aibase"))
	p := filepath.Join(h, "repo")
	os.MkdirAll(filepath.Join(p, ".git"), 0700)
	r := &gitRunner{}
	return p, &Inspector{Runner: r, Cache: cache.NewStore("")}, r
}
func TestInspectDeclarationsNoExecutionAndRedaction(t *testing.T) {
	p, i, r := setup(t)
	for name, s := range map[string]string{
		"package.json": `{"name":"demo","scripts":{"start":"touch SHOULD_NOT_EXIST; TOKEN=hidden"},"dependencies":{"react":"1"}}`,
		"Makefile":     "all: build\n\ttouch SHOULD_NOT_EXIST\n", "go.mod": "module example/demo\ngo 1.26\n",
		"pom.xml": "<project><artifactId>demo</artifactId></project>", "Cargo.toml": "[package]\nname = \"demo\"\n", "pyproject.toml": "[project.scripts]\nrun = \"demo:main\"\n",
		"build.gradle": "tasks.register('checkLocal') { exec { commandLine 'touch', 'SHOULD_NOT_EXIST' } }",
		"Dockerfile":   "FROM alpine\nRUN touch SHOULD_NOT_EXIST\nCMD [\"demo\"]", "compose.yaml": "services:\n  app:\n    image: demo\n    command: touch SHOULD_NOT_EXIST\n",
		"README.md": "# Demo title\n", "AGENTS.md": "do not execute me", ".env": "UNMARKED_PRIVATE_VALUE", ".ssh/key": "UNMARKED_PRIVATE_VALUE", "main.go": "", "src/main.py": "", "node_modules/ignored.js": "",
	} {
		write(t, p, name, s)
	}
	out := i.Inspect(context.Background(), p)
	b, _ := json.Marshal(out)
	s := string(b)
	for _, secret := range []string{"UNMARKED_PRIVATE_VALUE", "hidden", "user:secret", "git@host"} {
		if strings.Contains(s, secret) {
			t.Fatal("leak", s)
		}
	}
	if !strings.Contains(s, "touch SHOULD_NOT_EXIST") || !strings.Contains(s, "Demo title") {
		t.Fatal(s)
	}
	if out["git.dirty_count"].Value != float64(3) {
		t.Fatal(out["git.dirty_count"])
	}
	if _, e := os.Stat(filepath.Join(p, "SHOULD_NOT_EXIST")); !os.IsNotExist(e) {
		t.Fatal("declared command executed")
	}
	for _, c := range r.calls {
		if !strings.HasPrefix(c, "git ") || !strings.Contains(c, "core.fsmonitor=false") {
			t.Fatal(c)
		}
	}
	if out["languages"].Value.(map[string]any)["files"] != float64(2) {
		t.Fatal(out["languages"])
	}
}
func TestWalkLimitsDepthSymlinks(t *testing.T) {
	root := t.TempDir()
	for n := 0; n < 20005; n++ {
		write(t, root, fmt.Sprintf("f%05d.go", n), "")
	}
	n, cut, e := walk(context.Background(), root, MaxDepth, MaxFiles, func(string, fs.DirEntry, int) bool { return true })
	if e != nil || n != MaxFiles || !cut {
		t.Fatal(n, cut, e)
	}
	root = t.TempDir()
	write(t, root, "a/b/c/d/e/f/g/h/i/j.go", "")
	write(t, root, "node_modules/no.go", "")
	target := t.TempDir()
	write(t, target, "outside.go", "")
	t.Run("symlink", func(t *testing.T) { testutil.Symlink(t, target, filepath.Join(root, "alias")) })
	var files []string
	_, cut, e = walk(context.Background(), root, 8, MaxFiles, func(p string, d fs.DirEntry, _ int) bool {
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return true
	})
	if e != nil || !cut || len(files) != 0 {
		t.Fatal(files, cut, e)
	}
}
func TestRestrictedReadsAndRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".env", "secret")
	write(t, root, ".ssh/key", "secret")
	for _, p := range []string{".env", ".ssh/key"} {
		if _, e := read(filepath.Join(root, p)); e == nil {
			t.Fatal("sensitive read", p)
		}
	}
	t.Run("symlink", func(t *testing.T) {
		testutil.Symlink(t, filepath.Join(root, ".ssh", "key"), filepath.Join(root, "README.md"))
		if _, e := read(filepath.Join(root, "README.md")); e == nil {
			t.Fatal("symlink read")
		}
	})
	write(t, root, "big", strings.Repeat("a", MaxRead+1))
	if _, e := read(filepath.Join(root, "big")); e == nil {
		t.Fatal("oversize read")
	}
	if _, e := rootFor(filepath.Join(root, ".ssh")); e == nil {
		t.Fatal("sensitive root")
	}
}
func TestGitTimeoutBudget(t *testing.T) {
	p, i, r := setup(t)
	r.delay = true
	start := time.Now()
	out := i.Inspect(context.Background(), p)
	if time.Since(start) > 1800*time.Millisecond {
		t.Fatal("timeout budget exceeded")
	}
	for _, k := range []string{"branch", "remotes", "ahead_behind", "dirty_count", "commits"} {
		if out["git."+k].Status != cache.Timeout {
			t.Fatal(k, out)
		}
	}
}
func TestProjectsConfigCacheQueryAndDepth(t *testing.T) {
	p, i, r := setup(t)
	write(t, p, "main.go", "")
	base := filepath.Dir(p)
	deep := filepath.Join(base, "a/b/c/d/tooDeep")
	os.MkdirAll(filepath.Join(deep, ".git"), 0700)
	write(t, base, "Library/ignored/.git/config", "")
	b, _ := json.Marshal(map[string]any{"project_roots": []string{base}})
	write(t, cache.Home(), "config.json", string(b))
	f := i.Projects(context.Background(), "repo")
	if f.Status != cache.OK || f.TTL != 6*time.Hour {
		t.Fatal(f)
	}
	raw, _ := json.Marshal(f)
	if strings.Contains(string(raw), "tooDeep") || strings.Contains(string(raw), "ignored") {
		t.Fatal(string(raw))
	}
	if len(r.calls) != 0 {
		t.Fatal("discovery executed git", r.calls)
	}
	if !strings.Contains(string(raw), "not probed during discovery") {
		t.Fatal(string(raw))
	}
	before := len(r.calls)
	g := i.Projects(context.Background(), "not-found")
	if len(r.calls) != before {
		t.Fatal("cache miss")
	}
	raw, _ = json.Marshal(g.Value)
	if !strings.Contains(string(raw), `"projects":[]`) {
		t.Fatal(string(raw))
	}
	if !g.At.Equal(f.At) {
		t.Fatal("cache observation time reset")
	}
}
func TestMalformedManifestAndGitAbsent(t *testing.T) {
	p, i, _ := setup(t)
	os.Remove(filepath.Join(p, ".git"))
	write(t, p, "package.json", "{")
	out := i.Inspect(context.Background(), p)
	if out["package.json"].Status != cache.Unsupported || out["git.branch"].Status != cache.Missing {
		t.Fatal(out)
	}
}
