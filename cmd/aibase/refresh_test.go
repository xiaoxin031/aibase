package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xiaoxin031/aibase/internal/brief"
	"github.com/xiaoxin031/aibase/internal/cache"
)

func TestDetachedRefreshLifecycle(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "aibase")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	c := exec.Command("go", "build", "-o", bin, ".")
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("build: %s %v", b, e)
	}
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0700)
	env := []string{}
	for _, s := range os.Environ() {
		upper := strings.ToUpper(s)
		if !strings.HasPrefix(upper, "AIBASE_HOME=") && !strings.HasPrefix(upper, "HOME=") && !strings.HasPrefix(upper, "USERPROFILE=") && !strings.HasPrefix(upper, "PATH=") {
			env = append(env, s)
		}
	}
	env = append(env, "AIBASE_HOME="+home, "HOME="+home, "USERPROFILE="+home, "PATH=")
	hook := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, bin, "hook")
		c.Env = env
		c.Stdin = strings.NewReader("{}")
		b, e := c.CombinedOutput()
		if e != nil || !json.Valid(b) {
			t.Fatalf("hook: %s %v", b, e)
		}
	}
	store := cache.NewStore(home)
	cwd, _ := os.Getwd()
	wait := func(after time.Time) cache.Fact {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			f, ok := store.Get(brief.MachineKey)
			if ok && f.Fresh(time.Now()) && f.At.After(after) {
				if d, ok := store.Get("brief:doctor:" + cwd); !ok || d.At.Before(f.At) {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				return f
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("detached refresh did not complete")
		return cache.Fact{}
	}
	hook()
	f := wait(time.Time{})
	requestKey := "brief:refresh-request:v1:" + cwd
	before, ok := store.Get(requestKey)
	if !ok {
		t.Fatal("missing request")
	}
	// Wait for the child's skills write and lock release before testing the hot path.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := store.Get("brief:doctor:" + cwd); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	hook()
	after, _ := store.Get(requestKey)
	if !before.At.Equal(after.At) {
		t.Fatal("hot hook spawned refresh")
	}
	f.At = time.Now().Add(-2 * time.Minute)
	if e := store.Put(brief.MachineKey, f); e != nil {
		t.Fatal(e)
	}
	before.At = time.Now().Add(-time.Minute)
	if e := store.Put(requestKey, before); e != nil {
		t.Fatal(e)
	}
	hook()
	wait(f.At.Add(time.Minute))
}

func TestDetachedRefreshUsesContextDirectory(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "aibase")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %s %v", b, e)
	}
	home := filepath.Join(dir, "home")
	project := filepath.Join(dir, "project")
	if e := os.MkdirAll(project, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(project, "pom.xml"), []byte("<project/>"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AIBASE_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PATH", "")
	store := cache.NewStore(home)
	machine := map[string]cache.Fact{"os": cache.New("fixture", "test", time.Hour, cache.OK)}
	for key, value := range map[string]any{brief.MachineKey: machine, "skills:index:v1": []any{}} {
		if e := store.Put(key, cache.New(value, "test", time.Hour, cache.OK)); e != nil {
			t.Fatal(e)
		}
	}
	run := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, bin, "context", project, "--json")
		if b, e := c.CombinedOutput(); e != nil || !json.Valid(b) {
			t.Fatalf("context: %s %v", b, e)
		}
	}
	key := "brief:doctor:" + project
	wait := func(after time.Time) cache.Fact {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if f, ok := store.Get(key); ok && f.Fresh(time.Now()) && f.At.After(after) {
				b, _ := json.Marshal(f.Value)
				if !strings.Contains(string(b), "project.mvn") {
					t.Fatal(string(b))
				}
				return f
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("project doctor was not refreshed")
		return cache.Fact{}
	}
	run()
	f := wait(time.Time{})
	old := f.At
	f.At = time.Now().Add(-2 * time.Minute)
	if e := store.Put(key, f); e != nil {
		t.Fatal(e)
	}
	if e := store.Put("brief:refresh-request:v1:"+project, cache.New(true, "test", time.Nanosecond, cache.OK)); e != nil {
		t.Fatal(e)
	}
	run()
	wait(old)
}
