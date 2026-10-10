package host

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/redact"
)

const Budget = 1500 * time.Millisecond

// Windows process start-up (PowerShell, .cmd shims) is far slower than Unix fork/exec.
func DefaultBudget() time.Duration {
	if runtime.GOOS == "windows" {
		return 5 * time.Second
	}
	return Budget
}

func commandTimeout() time.Duration {
	if runtime.GOOS == "windows" {
		return 3 * time.Second
	}
	return Budget
}

const MaxOutput = 64 << 10

// Runner implementations must honor cancellation. Commands are argv, never shell text.
type Runner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) LookPath(s string) (string, error) { return exec.LookPath(s) }

type capped struct {
	mu sync.Mutex
	b  []byte
}

func (w *capped) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	left := MaxOutput - len(w.b)
	if left > 0 {
		w.b = append(w.b, p[:min(left, len(p))]...)
	}
	return n, nil
}
func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	configureProcess(c)
	c.WaitDelay = 20 * time.Millisecond
	c.Env = os.Environ()
	if strings.TrimSuffix(filepath.Base(name), ".exe") == "git" {
		env := []string{}
		for _, v := range c.Env {
			if !strings.HasPrefix(strings.ToUpper(v), "GIT_") {
				env = append(env, v)
			}
		}
		c.Env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	}
	c.Env = append(c.Env, "LC_ALL=C")
	w := &capped{}
	c.Stdout = w
	c.Stderr = w
	err := c.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return w.b, err
}

// FS permits synthetic /proc, /sys and registry-command tests. Reads must be bounded.
type FS interface {
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]fs.DirEntry, error)
	Stat(string) (fs.FileInfo, error)
	Readlink(string) (string, error)
}
type OSFS struct{}

func (OSFS) ReadFile(p string) ([]byte, error) {
	if redact.CheckPath(p) != nil {
		return nil, fs.ErrPermission
	}
	st, e := os.Stat(p)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fs.ErrPermission
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() {
		return nil, fs.ErrPermission
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(b) > 1<<20 {
		return nil, errors.New("file exceeds read limit")
	}
	return b, e
}
func (OSFS) ReadDir(p string) ([]fs.DirEntry, error) {
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	ds, e := f.ReadDir(20000)
	if e == io.EOF {
		e = nil
	}
	return ds, e
}
func (OSFS) Stat(p string) (fs.FileInfo, error) { return os.Stat(p) }
func (OSFS) Readlink(p string) (string, error)  { return os.Readlink(p) }
func Status(err error) cache.Status {
	switch {
	case err == nil:
		return cache.OK
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return cache.Timeout
	case errors.Is(err, fs.ErrPermission):
		return cache.Denied
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, exec.ErrNotFound):
		return cache.Missing
	default:
		return cache.Unsupported
	}
}
func trim(b []byte) string { return strings.TrimSpace(string(b)) }
