package host

import (
	"context"
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/cache"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

type fakeFS struct {
	fstest.MapFS
	links map[string]string
}

func (f fakeFS) ReadFile(p string) ([]byte, error) {
	return fs.ReadFile(f.MapFS, strings.TrimPrefix(filepath.ToSlash(p), "/"))
}
func (f fakeFS) ReadDir(p string) ([]fs.DirEntry, error) {
	return fs.ReadDir(f.MapFS, strings.TrimPrefix(filepath.ToSlash(p), "/"))
}
func (f fakeFS) Stat(p string) (fs.FileInfo, error) {
	return fs.Stat(f.MapFS, strings.TrimPrefix(filepath.ToSlash(p), "/"))
}
func (f fakeFS) Readlink(p string) (string, error) {
	if s, ok := f.links[filepath.ToSlash(p)]; ok {
		return s, nil
	}
	return "", fs.ErrNotExist
}

type fakeRunner struct {
	active, peak, calls atomic.Int32
	delay               time.Duration
	mu                  sync.Mutex
	names               []string
}

func (r *fakeRunner) LookPath(n string) (string, error) { return "/bin/tool", nil }
func (r *fakeRunner) Run(ctx context.Context, dir, n string, a ...string) ([]byte, error) {
	r.calls.Add(1)
	v := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		old := r.peak.Load()
		if old >= v || r.peak.CompareAndSwap(old, v) {
			break
		}
	}
	r.mu.Lock()
	r.names = append(r.names, n+" "+strings.Join(a, " "))
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(r.delay):
		return []byte("version TOKEN=private"), nil
	}
}
func fixture(t *testing.T) (*Inspector, *fakeRunner) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", filepath.Join(h, ".aibase"))
	f := fakeFS{MapFS: fstest.MapFS{"bin/tool": &fstest.MapFile{Data: []byte("tool"), Mode: 0755, ModTime: time.Unix(1, 0)}}}
	r := &fakeRunner{}
	return &Inspector{Runner: r, FS: f, Env: func(string) string { return "" }, Home: h}, r
}
func TestBudgetConcurrencyAndL0(t *testing.T) {
	i, r := fixture(t)
	i.Budget = Budget
	r.delay = 2 * time.Second
	start := time.Now()
	out := i.Inspect(context.Background(), 0)
	if time.Since(start) > 1800*time.Millisecond {
		t.Fatal("budget exceeded")
	}
	if r.peak.Load() > 8 {
		t.Fatal(r.peak.Load())
	}
	if out["tool.go"].Status != cache.Timeout {
		t.Fatal(out["tool.go"])
	}
	if _, ok := out["listeners"]; ok {
		t.Fatal("L1 requested in L0")
	}
}
func TestInjectedRunnerRedactionAndToolCache(t *testing.T) {
	i, r := fixture(t)
	i.Cache = cache.NewStore(t.TempDir())
	out := i.Inspect(context.Background(), 1)
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "private") {
		t.Fatal(string(b))
	}
	if out["tool.go"].Status != cache.OK {
		t.Fatal(out["tool.go"])
	}
	before := r.calls.Load()
	i.Inspect(context.Background(), 0)
	r.mu.Lock()
	toolCalls := 0
	for _, n := range r.names[int(before):] {
		if strings.HasPrefix(n, "/bin/tool ") {
			toolCalls++
		}
	}
	r.mu.Unlock()
	if toolCalls > 0 {
		t.Fatal("version cache missed", r.calls.Load()-before)
	}
	f := i.FS.(fakeFS)
	f.MapFS["bin/tool"].ModTime = time.Unix(2, 0)
	i.Inspect(context.Background(), 0)
	if r.calls.Load() == before {
		t.Fatal("mtime invalidation missed")
	}
}
func TestProcListeners(t *testing.T) {
	i, _ := fixture(t)
	i.FS = fakeFS{MapFS: fstest.MapFS{
		"proc/net/tcp": {Data: []byte("0: 0100007F:1F90 00000000:0000 0A 0:0 0:0 0 1000 0 123\n")}, "proc/net/tcp6": {Data: []byte("")}, "proc/42/fd/3": {}, "proc/42/comm": {Data: []byte("server")}, "proc/42/status": {Data: []byte("Name:\tserver\nVmRSS:\t2048 kB\n")}}, links: map[string]string{"/proc/42/fd/3": "socket:[123]"}}
	v, e := i.linuxListeners(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ls := v.([]Listener)
	if len(ls) != 1 || ls[0].Address != "127.0.0.1" || ls[0].Port != 8080 || ls[0].Processes[0].PID != 42 {
		t.Fatal(ls)
	}
	a, p, e := procAddress("00000000000000000000000001000000:01BB")
	if e != nil || a != "::1" || p != 443 {
		t.Fatal(a, p, e)
	}
}
func TestSensitiveReadAndCommandCancellation(t *testing.T) {
	h := t.TempDir()
	p := filepath.Join(h, ".env")
	os.WriteFile(p, []byte("NEVER_READ"), 0600)
	if _, e := (OSFS{}).ReadFile(p); e == nil {
		t.Fatal("sensitive read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (ExecRunner{}).Run(ctx, "", os.Args[0], "-test.run=^$"); e != context.Canceled {
		t.Fatal(e)
	}
}

func TestVersionArgumentsAndLocalDocker(t *testing.T) {
	for name, want := range map[string]string{"go": "version", "java": "-version", "ffmpeg": "-version", "ffprobe": "-version", "magick": "-version", "convert": "-version", "exiftool": "-ver", "kubectl": "version --client"} {
		if got := strings.Join(versionArgs(name), " "); got != want {
			t.Fatal(name, got)
		}
	}
	i, r := fixture(t)
	i.Env = func(k string) string {
		if k == "DOCKER_HOST" {
			return "tcp://remote.example:2375"
		}
		return ""
	}
	p := i.containers()
	p.run(context.Background())
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.Contains(r.names[0], "remote.example") || !strings.Contains(r.names[0], "--host") {
		t.Fatal(r.names)
	}
}

func TestBasicDoesNotProbeTools(t *testing.T) {
	i, r := fixture(t)
	out, e := i.InspectLevel(context.Background(), "basic")
	if e != nil {
		t.Fatal(e)
	}
	for k := range out {
		if strings.HasPrefix(k, "tool.") {
			t.Fatal(k)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cmd := range r.names {
		if strings.HasPrefix(cmd, "/bin/tool") {
			t.Fatal("basic probed tool version")
		}
	}
}
