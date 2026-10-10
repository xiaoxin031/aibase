package project

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func indexEntries(t *testing.T, f cache.Fact) indexValue {
	t.Helper()
	b, e := json.Marshal(f.Value)
	if e != nil {
		t.Fatal(e)
	}
	var v indexValue
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func repo(t *testing.T, p string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Join(p, ".git"), 0700); e != nil {
		t.Fatal(e)
	}
}
func TestIndexSeedsNoiseHiddenAndNoGit(t *testing.T) {
	_, i, r := setup(t)
	home, _ := os.UserHomeDir()
	for n := 0; n < MaxFiles+10; n++ {
		write(t, home, fmt.Sprintf("Desktop/noise-%05d", n), "")
	}
	repo(t, filepath.Join(home, "Desktop"))
	wanted := []string{filepath.Join(home, "code", "later-seed"), filepath.Join(home, "Desktop", "real-repo"), filepath.Join(home, ".explicit", "hidden-repo")}
	for _, p := range wanted {
		repo(t, p)
	}
	repo(t, filepath.Join(home, "Desktop", ".hidden", "ignored"))
	repo(t, filepath.Join(home, "Pictures", "ignored"))
	f := i.Projects(context.Background(), "")
	v := indexEntries(t, f)
	for _, p := range wanted[:2] {
		found := false
		for _, e := range v.Projects {
			found = found || e.Path.Value == p
		}
		if !found {
			t.Fatal("missing seed", p, f)
		}
	}
	for _, e := range v.Projects {
		if strings.Contains(fmt.Sprint(e.Path.Value), "ignored") || e.Path.Value == wanted[2] {
			t.Fatal(e)
		}
	}
	if len(r.calls) != 0 {
		t.Fatal(r.calls)
	}
	config, _ := json.Marshal(map[string]any{"project_roots": []string{filepath.Join(home, ".explicit")}})
	write(t, cache.Home(), "config.json", string(config))
	f = i.Projects(context.Background(), "hidden-repo")
	if len(indexEntries(t, f).Projects) != 1 {
		t.Fatal(f)
	}
}
func TestIndexMemorySeedsAndRootStop(t *testing.T) {
	_, i, _ := setup(t)
	home, _ := os.UserHomeDir()
	other := t.TempDir()
	a := filepath.Join(other, "history-repo")
	b := filepath.Join(other, "saved-repo")
	repo(t, a)
	repo(t, b)
	repo(t, filepath.Join(a, "nested"))
	line, _ := json.Marshal(map[string]string{"project": "hash-id", "path": a})
	write(t, cache.Home(), "history.jsonl", string(line)+"\n")
	write(t, cache.Home(), "projects/id/path", b)
	f := i.Projects(context.Background(), "")
	v := indexEntries(t, f)
	found := map[string]bool{}
	for _, e := range v.Projects {
		found[fmt.Sprint(e.Path.Value)] = true
	}
	if !found[a] || !found[b] || found[filepath.Join(a, "nested")] {
		t.Fatal(home, f)
	}
}
func TestIndexIncrementalTimeoutAndQuery(t *testing.T) {
	p, i, _ := setup(t)
	base := filepath.Dir(p)
	first := i.Projects(context.Background(), "", base)
	if len(indexEntries(t, first).Projects) != 1 {
		t.Fatal(first)
	}
	files, e := os.ReadDir(i.Cache.Dir)
	if e != nil {
		t.Fatal(e)
	}
	// Expire the existing cache to force a new scan, preserving its old entries.
	for _, file := range files {
		path := filepath.Join(i.Cache.Dir, file.Name())
		b, _ := os.ReadFile(path)
		var f cache.Fact
		if json.Unmarshal(b, &f) == nil {
			f.At = time.Now().Add(-7 * time.Hour)
			b, _ = json.Marshal(f)
			if e := os.WriteFile(path, b, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	newPath := filepath.Join(base, "new-repo")
	repo(t, newPath)
	second := i.Projects(context.Background(), "", base)
	if len(indexEntries(t, second).Projects) != 2 {
		t.Fatal(second)
	}
	for _, file := range files {
		path := filepath.Join(i.Cache.Dir, file.Name())
		b, _ := os.ReadFile(path)
		var f cache.Fact
		_ = json.Unmarshal(b, &f)
		f.At = time.Now().Add(-7 * time.Hour)
		b, _ = json.Marshal(f)
		_ = os.WriteFile(path, b, 0600)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	timed := i.Projects(ctx, "", base)
	if timed.Status != cache.Timeout || len(indexEntries(t, timed).Projects) != 2 || time.Since(start) > time.Second {
		t.Fatal(timed)
	}
	cached, _ := os.ReadFile(filepath.Join(i.Cache.Dir, files[0].Name()))
	if !strings.Contains(string(cached), `"timeout"`) {
		t.Fatal(string(cached))
	}
	q := filter(cache.New(indexValue{Projects: []Entry{{Name: fact("sample", "", nil), Path: fact("/a/sample", "", nil), Language: fact("query-marker", "", nil)}}}, "", 0, cache.OK), "query-marker")
	if len(indexEntries(t, q).Projects) != 0 {
		t.Fatal(q)
	}
}
func TestRealEmptyGitAndFailure(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = h
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%v: %s %v", args, b, e)
		}
	}
	run("init")
	i := New()
	out := Report{}
	i.git(context.Background(), h, out)
	for _, k := range []string{"branch", "remotes", "ahead_behind", "dirty_count", "commits"} {
		if out["git."+k].Status != cache.OK {
			t.Fatal(k, out)
		}
	}
	if out["git.commits"].Value != "no commits yet" || out["git.ahead_behind"].Value != "no upstream" {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out["git.remotes"].Value)
	if string(b) != "[]" {
		t.Fatal(string(b))
	}
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	out = Report{}
	i.git(context.Background(), h, out)
	if out["git.ahead_behind"].Status != cache.OK || out["git.ahead_behind"].Value != "no upstream" {
		t.Fatal(out)
	}
	write(t, h, ".git/HEAD", "corrupt head\n")
	out = Report{}
	i.git(context.Background(), h, out)
	if out["git.commits"].Status == cache.OK || out["git.ahead_behind"].Status == cache.OK {
		t.Fatal("git error normalized", out)
	}
}
func TestManifestSummariesAndText(t *testing.T) {
	v, e := parse("Makefile", []byte("all test: dep\n\ttouch SECRET_RECIPE\nVAR := hidden\n"))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if string(b) != `{"targets":["all","test"]}` {
		t.Fatal(string(b))
	}
	input, _ := json.Marshal(map[string]any{"name": "not-needed", "scripts": map[string]string{"test": strings.Repeat("字", 150)}, "dependencies": map[string]string{"raw": "discard"}})
	v, e = parse("package.json", input)
	if e != nil {
		t.Fatal(e)
	}
	scripts := v.(map[string]any)["scripts"].(map[string]string)
	if len([]rune(scripts["test"])) != 120 {
		t.Fatal(scripts)
	}
	for _, name := range Manifests {
		if name == "package.json" || name == "pom.xml" {
			continue
		}
		v, e := parse(name, []byte("# RAW_SENTINEL\n"))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "RAW_SENTINEL") || strings.Contains(string(b), `"declaration"`) {
			t.Fatal(name, string(b))
		}
	}
	r := Report{"Makefile": fact(v, "", nil), "package.json": cache.New(nil, "", 0, cache.Missing), "git.commits": fact("no commits yet", "", nil)}
	text := Text(r)
	if strings.Contains(text, "package.json") || !strings.Contains(text, "no commits yet") {
		t.Fatal(text)
	}
	b, _ = json.Marshal(r)
	if !strings.Contains(string(b), "package.json") {
		t.Fatal(string(b))
	}
}

func TestPartialDiscoveryPersistsWithinBudget(t *testing.T) {
	_, i, _ := setup(t)
	home, _ := os.UserHomeDir()
	first := filepath.Join(home, "first")
	noise := filepath.Join(home, "noise")
	repo(t, first)
	for n := 0; n < 4000; n++ {
		if e := os.MkdirAll(filepath.Join(noise, fmt.Sprintf("d%04d", n)), 0700); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	f := i.Projects(ctx, "", first, noise)
	if f.Status != cache.Timeout || !indexEntries(t, f).Truncated {
		t.Fatal("expected partial scan", f)
	}
	if len(indexEntries(t, f).Projects) != 1 {
		t.Fatal("lost discoveries", f)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("deadline not respected")
	}
	fs, e := os.ReadDir(i.Cache.Dir)
	if e != nil || len(fs) != 1 {
		t.Fatal(fs, e)
	}
	b, e := os.ReadFile(filepath.Join(i.Cache.Dir, fs[0].Name()))
	encodedPath, _ := json.Marshal(first)
	if e != nil || !strings.Contains(string(b), string(encodedPath)) {
		t.Fatal("partial cache missing", e)
	}
}

func TestReadableProjectOutput(t *testing.T) {
	r := Report{"Makefile": cache.New(map[string]any{"targets": []string{"build", "test"}}, "fixture", 0, cache.OK)}
	if got := Text(r); got != "Makefile: targets=build, test" {
		t.Fatal(got)
	}
	f := cache.New(indexValue{Projects: []Entry{{Name: fact("demo", "", nil), Path: fact("/work/demo", "", nil)}}}, "fixture", 0, cache.OK)
	if got := ProjectsText(f); got != "demo: /work/demo" {
		t.Fatal(got)
	}
}
