package brief

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/doctor"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/project"
	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

const MachineKey = "brief:machine:v2"

var ErrRefreshBusy = errors.New("refresh already running")

// Refresh synchronously fills the same bounded snapshot consumed by hooks.
// Install/setup can call this after configuring the data directory.
func Refresh(ctx context.Context, level string) (host.Report, error) {
	if level != "basic" && level != "tools" {
		return nil, errors.New("level must be basic or tools")
	}
	store := cache.NewStore("")
	unlock, err := refreshLock(store)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return refresh(ctx, store, skills.Root(false), level)
}
func refresh(ctx context.Context, store *cache.Store, root, level string) (host.Report, error) {
	i := host.New()
	i.Cache = store
	r, e := i.InspectLevel(ctx, level)
	if e != nil {
		return nil, e
	}
	if e = store.Put(MachineKey, cache.New(r, "refresh "+level, time.Minute, cache.OK)); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	all, e := skills.ListContext(ctx, root)
	if e != nil {
		return r, e
	}
	if e = putSkillIndex(store, all); e != nil {
		return r, e
	}
	dir, e := os.Getwd()
	if e != nil {
		return r, e
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		return r, e
	}
	if root := memory.ProjectRoot(dir); root != "" {
		pctx, pcancel := context.WithTimeout(context.Background(), time.Second)
		summary := ProjectSummary(project.New().Inspect(pctx, root))
		pcancel()
		_ = store.Put(ProjectKey(root), cache.New(summary, "refresh inspect project", 10*time.Minute, cache.OK))
	}
	gaps := []doctor.Check{}
	for _, check := range doctor.Quick(dir) {
		if check.Status != "ok" {
			gaps = append(gaps, check)
		}
	}
	return r, store.Put("brief:doctor:"+dir, cache.New(gaps, "refresh quick doctor", time.Minute, cache.OK))
}
func refreshLock(store *cache.Store) (func(), error) {
	if e := redact.CheckPath(store.Dir); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(store.Dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(store.Dir, "refresh.lock")
	if e := redact.CheckPath(path); e != nil {
		return nil, e
	}
	return lockFile(path)
}

// WaitRefresh is retained for callers that used the former in-process worker.
func WaitRefresh(ctx context.Context) error { return ctx.Err() }
func startRefresh(store *cache.Store, dir string) {
	// A test executable cannot dispatch the CLI refresh command.
	exe, e := os.Executable()
	if e != nil || (strings.HasSuffix(exe, ".test") || strings.HasSuffix(exe, ".test.exe")) {
		return
	}
	unlock, e := refreshLock(store)
	if e != nil {
		return
	}
	defer func() { unlock() }()
	// Throttle launches before the child acquires its own process lock.
	requestKey := "brief:refresh-request:v1:" + dir
	if f, ok := store.GetBounded(requestKey, 1024); ok && f.Fresh(time.Now()) {
		return
	}
	if e := store.Put(requestKey, cache.New(true, "refresh request", 5*time.Second, cache.OK)); e != nil {
		return
	}
	unlock()
	unlock = func() {}
	c := exec.Command(exe, "refresh", "--level", "tools")
	c.Dir = dir
	c.Env = append(c.Environ(), "AIBASE_HOME="+filepath.Dir(store.Dir))
	detach(c)
	// nil stdio is connected to the null device by os/exec.
	if e = c.Start(); e != nil {
		return
	}
	go func() { _ = c.Wait() }()
}
