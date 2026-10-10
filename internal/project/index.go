package project

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/redact"
)

type Entry struct {
	Name         cache.Fact `json:"name"`
	Path         cache.Fact `json:"path"`
	Language     cache.Fact `json:"language"`
	RecentCommit cache.Fact `json:"recent_commit"`
}
type indexValue struct {
	Projects       []Entry `json:"projects"`
	Truncated      bool    `json:"truncated"`
	ScannedEntries int     `json:"scanned_entries"`
}

// Projects discovers repositories before any optional metadata work. A partial
// scan is persisted and merged on the next request, never treated as complete.
func (i *Inspector) Projects(ctx context.Context, query string, roots ...string) cache.Fact {
	ctx, cancel := context.WithTimeout(ctx, host.Budget)
	defer cancel()
	data := cache.Home()
	if i.Cache != nil {
		data = filepath.Dir(i.Cache.Dir)
	}
	if len(roots) == 0 {
		b, e := read(filepath.Join(data, "config.json"))
		if e == nil {
			var c struct {
				Roots []string `json:"project_roots"`
			}
			if json.Unmarshal(b, &c) != nil {
				return cache.New(nil, "config.json", 0, cache.Unsupported)
			}
			roots = c.Roots
		} else if !os.IsNotExist(e) {
			return fact(nil, "config.json", e)
		}
	}
	defaults := len(roots) == 0
	if defaults {
		h, e := os.UserHomeDir()
		if e != nil {
			return fact(nil, "home", e)
		}
		roots = []string{h}
	}
	roots = append([]string(nil), roots...)
	for n, r := range roots {
		a, e := filepath.Abs(r)
		if e != nil {
			return fact(nil, "project_roots", e)
		}
		roots[n] = a
	}
	raw, _ := json.Marshal(roots)
	key := fmt.Sprintf("projects:v3:%t:%x", defaults, sha256.Sum256(raw))
	prior := indexValue{}
	if i.Cache != nil {
		if f, ok := i.Cache.Get(key); ok {
			b, _ := json.Marshal(f.Value)
			_ = json.Unmarshal(b, &prior)
			if f.Status == cache.OK && !prior.Truncated && f.Fresh(time.Now()) {
				return filter(f, query)
			}
		}
	}
	seeds := roots
	if defaults {
		seeds = nil
		for _, name := range []string{"Desktop", "Documents", "dev", "code", "src", "projects", "repos", "work", "workspace", "go/src", "github", "git"} {
			seed := filepath.Join(roots[0], filepath.FromSlash(name))
			seeds = append(seeds, seed)
			// A development container may itself have git metadata (e.g. Desktop).
			// Its immediate directories remain independent priority seeds.
			if st, e := os.Lstat(filepath.Join(seed, ".git")); e == nil && (st.IsDir() || st.Mode().IsRegular()) {
				if f, e := os.Open(seed); e == nil {
					for ctx.Err() == nil {
						ds, err := f.ReadDir(128)
						for _, d := range ds {
							if d.IsDir() && d.Type()&os.ModeSymlink == 0 && !indexSkip(d.Name()) {
								seeds = append(seeds, filepath.Join(seed, d.Name()))
							}
						}
						if err != nil {
							break
						}
					}
					f.Close()
				}
			}
		}
		seeds = append(seeds, memorySeeds(ctx, data)...)
		if runtime.GOOS == "linux" {
			seeds = append(seeds, "/srv", "/opt", "/var/www", "/root")
		}
	}
	type node struct {
		path  string
		depth int
	}
	queue := []node{}
	seen := map[string]bool{}
	for _, p := range seeds {
		queue = append(queue, node{p, 0})
	}
	entries := map[string]Entry{}
	for _, e := range prior.Projects {
		if p, ok := e.Path.Value.(string); ok {
			entries[p] = e
		}
	}
	scanned := 0
	truncated := false
	var scanErr error
	fallback := defaults
	for head := 0; ; head++ {
		if head == len(queue) {
			if !fallback {
				break
			}
			fallback = false
			for _, p := range roots {
				queue = append(queue, node{p, 0})
			}
		}
		if ctx.Err() != nil {
			truncated = true
			break
		}
		n := queue[head]
		p := filepath.Clean(n.path)
		if seen[p] || redact.CheckPath(p) != nil {
			continue
		}
		seen[p] = true
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if st, e := os.Lstat(filepath.Join(p, ".git")); e == nil && (st.IsDir() || st.Mode().IsRegular()) {
			entries[p] = Entry{cache.New(filepath.Base(p), "directory", 6*time.Hour, cache.OK), cache.New(p, "filesystem", 6*time.Hour, cache.OK), cache.New(nil, "not probed during discovery", 6*time.Hour, cache.Unsupported), cache.New(nil, "not probed during discovery", 6*time.Hour, cache.Unsupported)}
			continue
		}
		if n.depth >= 4 {
			continue
		}
		f, e := os.Open(p)
		if e != nil {
			if !defaults {
				scanErr = e
			}
			continue
		}
		for ctx.Err() == nil {
			ds, err := f.ReadDir(128)
			for _, d := range ds {
				scanned++
				if !d.IsDir() || d.Type()&os.ModeSymlink != 0 || indexSkip(d.Name()) {
					continue
				}
				queue = append(queue, node{filepath.Join(p, d.Name()), n.depth + 1})
			}
			if err != nil {
				if err != io.EOF && !defaults {
					scanErr = err
				}
				break
			}
		}
		f.Close()
	}
	if ctx.Err() != nil {
		scanErr = ctx.Err()
		truncated = true
	}
	out := indexValue{Projects: []Entry{}, Truncated: truncated, ScannedEntries: scanned}
	for _, e := range entries {
		out.Projects = append(out.Projects, e)
	}
	sort.Slice(out.Projects, func(a, b int) bool {
		return fmt.Sprint(out.Projects[a].Path.Value) < fmt.Sprint(out.Projects[b].Path.Value)
	})
	f := cache.New(out, "repository index", 6*time.Hour, host.Status(scanErr))
	if i.Cache != nil {
		_ = i.Cache.Put(key, f)
	}
	return filter(f, query)
}
func indexSkip(name string) bool {
	if strings.HasPrefix(name, ".") || skip[name] {
		return true
	}
	switch name {
	case "Applications", "Pictures", "Movies", "Music":
		return true
	}
	return false
}

// Read only a bounded recent tail and the small path records; memory text is
// never interpreted as instructions or used to execute commands.
func memorySeeds(ctx context.Context, data string) []string {
	out := []string{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
		}
	}
	p := filepath.Join(data, "history.jsonl")
	if redact.CheckPath(p) == nil {
		if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() {
			if f, e := os.Open(p); e == nil {
				start := max(int64(0), st.Size()-MaxRead)
				_, _ = f.Seek(start, io.SeekStart)
				b, _ := io.ReadAll(io.LimitReader(f, MaxRead))
				f.Close()
				lines := strings.Split(string(b), "\n")
				if start > 0 && len(lines) > 0 {
					lines = lines[1:]
				}
				for j := len(lines) - 1; j >= 0 && ctx.Err() == nil; j-- {
					var v struct {
						Project string `json:"project"`
						Path    string `json:"path"`
						Cwd     string `json:"cwd"`
					}
					if json.Unmarshal([]byte(lines[j]), &v) == nil {
						add(v.Path)
						add(v.Project)
						add(v.Cwd)
					}
				}
			}
		}
	}
	p = filepath.Join(data, "projects")
	if redact.CheckPath(p) != nil {
		return out
	}
	f, e := os.Open(p)
	if e != nil {
		return out
	}
	defer f.Close()
	for ctx.Err() == nil {
		ds, e := f.ReadDir(128)
		for _, d := range ds {
			if ctx.Err() != nil {
				break
			}
			if d.IsDir() && d.Type()&os.ModeSymlink == 0 {
				if b, e := read(filepath.Join(p, d.Name(), "path")); e == nil {
					add(string(b))
				}
			}
		}
		if e != nil {
			break
		}
	}
	return out
}
func filter(f cache.Fact, q string) cache.Fact {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return f
	}
	b, _ := json.Marshal(f.Value)
	var v indexValue
	if json.Unmarshal(b, &v) != nil {
		return f
	}
	out := []Entry{}
	for _, e := range v.Projects {
		name, _ := e.Name.Value.(string)
		path, _ := e.Path.Value.(string)
		if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(path), q) {
			out = append(out, e)
		}
	}
	v.Projects = out
	f.Value = cache.Clean(v)
	return f
}
