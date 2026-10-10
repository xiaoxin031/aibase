package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type settings struct {
	Inject      bool `json:"inject"`
	Autosave    bool `json:"autosave"`
	ReviewEvery int  `json:"review_every"`
}
type status struct {
	Version string    `json:"version"`
	DataDir string    `json:"data_dir"`
	Config  *settings `json:"config"`
	Clients []struct {
		Name       string `json:"name"`
		Detected   bool   `json:"detected"`
		Configured bool   `json:"configured"`
		Error      string `json:"error"`
	} `json:"clients"`
	Memory []struct {
		Target string `json:"target"`
		Used   int    `json:"used"`
		Limit  int    `json:"limit"`
		Error  string `json:"error"`
	} `json:"memory"`
	Sessions []struct {
		TS    string `json:"ts"`
		Agent string `json:"agent"`
		Text  string `json:"text"`
	} `json:"sessions"`
	LastInjection *struct {
		TS     string `json:"ts"`
		Client string `json:"client"`
	} `json:"last_injection"`
	Doctor struct {
		Counts map[string]int `json:"counts"`
	} `json:"doctor"`
}

func findCore(executable string) (string, error) {
	name := "aibase"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Resolve symlinks before looking beside the executable (including app bundles).
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		executable = real
	}
	adjacent := filepath.Join(filepath.Dir(executable), name)
	if st, err := os.Stat(adjacent); err == nil && st.Mode().IsRegular() && (runtime.GOOS == "windows" || st.Mode()&0111 != 0) {
		return adjacent, nil
	}
	return exec.LookPath(name)
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("aibase output exceeds limit")
	}
	return b.Buffer.Write(p)
}

type client struct {
	path    string
	timeout time.Duration
}

func (c client) status(ctx context.Context) (status, error) {
	var s status
	if err := c.call(ctx, &s, "status"); err != nil {
		return s, err
	}
	if s.Version == "" || !filepath.IsAbs(s.DataDir) || s.Config == nil || s.Doctor.Counts == nil {
		return s, errors.New("incomplete aibase status; update the core executable")
	}
	return s, nil
}

func (c client) call(parent context.Context, result any, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.path, append(args, "--json")...)
	prepareCommand(cmd)
	cmd.WaitDelay = time.Second
	out := &limitedBuffer{limit: 2 << 20}
	stderr := &limitedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("aibase: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("aibase %s: %w", args[0], err)
	}
	if err := json.Unmarshal(out.Bytes(), result); err != nil {
		return fmt.Errorf("invalid aibase JSON: %w", err)
	}
	return nil
}

func label(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 110 {
		s = string(r[:107]) + "..."
	}
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(s, "&", "&&")
	}
	return s
}

func openPath(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("expected absolute path")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", path)
	case "windows":
		// Pass the path via an environment variable, never interpolate it into code.
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Process -FilePath $env:AIBASE_OPEN_PATH")
		cmd.Env = append(os.Environ(), "AIBASE_OPEN_PATH="+path)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", path)
	}
	prepareCommand(cmd)
	cmd.WaitDelay = time.Second
	return cmd.Run()
}
