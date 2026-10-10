package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFindCoreAdjacentAndFallback(t *testing.T) {
	dir := t.TempDir()
	name := "aibase"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	got, err := findCore(filepath.Join(dir, "tray"))
	if err != nil || got != file {
		t.Fatal(got, err)
	}
	t.Setenv("PATH", dir)
	got, err = findCore(filepath.Join(t.TempDir(), "tray"))
	if err != nil || got != file {
		t.Fatal(got, err)
	}
}
func TestOutputBoundAndLabels(t *testing.T) {
	b := limitedBuffer{limit: 4}
	if _, e := b.Write([]byte("1234")); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Write([]byte("5")); e == nil {
		t.Fatal("unbounded output")
	}
	want := "a b&c"
	if runtime.GOOS == "windows" {
		want = "a b&&c"
	}
	if got := label("a\n b&c"); got != want {
		t.Fatal(got)
	}
	if len([]rune(label(strings.Repeat("中", 200)))) != 110 {
		t.Fatal("rune truncation")
	}
}
func TestMain(m *testing.M) {
	if mode := os.Getenv("AIBASE_TRAY_TEST_HELPER"); mode != "" {
		switch mode {
		case "config":
			if strings.Join(os.Args[1:], " ") != "config inject false --json" {
				os.Exit(9)
			}
			fmt.Print(`{"inject":false,"autosave":true,"review_every":8}`)
		case "invalid":
			fmt.Print("not json")
		case "empty":
			fmt.Print("{}")
		case "failure":
			fmt.Print("{}")
			os.Exit(3)
		case "wait":
			time.Sleep(30 * time.Second)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestCommandJSONAndTimeout(t *testing.T) {
	c := client{path: os.Args[0], timeout: 5 * time.Second}
	t.Setenv("AIBASE_TRAY_TEST_HELPER", "config")
	var cfg settings
	if err := c.call(context.Background(), &cfg, "config", "inject", "false"); err != nil || cfg.Inject || !cfg.Autosave {
		t.Fatal(cfg, err)
	}
	t.Setenv("AIBASE_TRAY_TEST_HELPER", "invalid")
	if err := c.call(context.Background(), &cfg, "status"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	t.Setenv("AIBASE_TRAY_TEST_HELPER", "empty")
	if _, err := c.status(context.Background()); err == nil {
		t.Fatal("incomplete status accepted")
	}
	t.Setenv("AIBASE_TRAY_TEST_HELPER", "failure")
	if err := c.call(context.Background(), &cfg, "status"); err == nil {
		t.Fatal("nonzero exit accepted")
	}
	t.Setenv("AIBASE_TRAY_TEST_HELPER", "wait")
	c.timeout = 100 * time.Millisecond
	if err := c.call(context.Background(), &cfg, "status"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.call(ctx, &cfg, "status"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestCoreContract(t *testing.T) {
	core := os.Getenv("AIBASE_TEST_CORE")
	if core == "" {
		t.Skip("set AIBASE_TEST_CORE to a built core executable")
	}
	t.Setenv("AIBASE_HOME", t.TempDir())
	c := client{path: core, timeout: 10 * time.Second}
	s, err := c.status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Config.Inject || !s.Config.Autosave || s.DataDir == "" || len(s.Clients) == 0 {
		t.Fatalf("incomplete status: %+v", s)
	}
	var cfg settings
	if err := c.call(context.Background(), &cfg, "config", "inject", "false"); err != nil || cfg.Inject {
		t.Fatal(cfg, err)
	}
	if err := c.call(context.Background(), &cfg, "config", "autosave", "false"); err != nil || cfg.Autosave || cfg.ReviewEvery != -1 {
		t.Fatal(cfg, err)
	}
	if err := c.call(context.Background(), &cfg, "config", "autosave", "true"); err != nil || !cfg.Autosave || cfg.ReviewEvery != 8 {
		t.Fatal(cfg, err)
	}
	var report json.RawMessage
	if err := c.call(context.Background(), &report, "doctor"); err != nil {
		t.Fatal(err)
	}
}
