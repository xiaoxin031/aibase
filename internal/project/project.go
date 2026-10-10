// Package project reads bounded project metadata. Declared scripts, recipes and
// container commands are data only. Only fixed, read-only git argv are executed.
package project

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/redact"
)

const MaxFiles = 20000
const MaxDepth = 8
const MaxRead = 256 << 10

type Report map[string]cache.Fact
type Inspector struct {
	Runner host.Runner
	Cache  *cache.Store
}

func New() *Inspector                              { return &Inspector{host.ExecRunner{}, cache.NewStore("")} }
func fact(v any, src string, err error) cache.Fact { return cache.New(v, src, 0, host.Status(err)) }
func read(path string) ([]byte, error) {
	if redact.CheckPath(path) != nil {
		return nil, fs.ErrPermission
	}
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fs.ErrPermission
	}
	if st.Size() > MaxRead {
		return nil, errors.New("metadata exceeds read limit")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxRead+1))
	if len(b) > MaxRead {
		return nil, errors.New("metadata exceeds read limit")
	}
	return b, e
}

var skip = map[string]bool{"node_modules": true, ".git": true, "vendor": true, "target": true, "dist": true, "build": true, ".cache": true, "__pycache__": true, ".venv": true, "venv": true, "Library": true, "AppData": true, "Caches": true, ".aibase": true, ".amem": true}

// walk streams directory entries so a single huge directory cannot bypass limits.
func walk(ctx context.Context, root string, depth, limit int, visit func(string, fs.DirEntry, int) bool) (int, bool, error) {
	count := 0
	truncated := false
	var scan func(string, int) error
	scan = func(dir string, d int) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		f, e := os.Open(dir)
		if e != nil {
			return e
		}
		defer f.Close()
		for {
			entries, e := f.ReadDir(128)
			for _, entry := range entries {
				if er := ctx.Err(); er != nil {
					return er
				}
				if count >= limit {
					truncated = true
					return nil
				}
				count++
				p := filepath.Join(dir, entry.Name())
				descend := visit(p, entry, d)
				if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !descend || skip[entry.Name()] || redact.Forbidden(p) {
					continue
				}
				if d >= depth {
					truncated = true
					continue
				}
				if er := scan(p, d+1); er != nil {
					return er
				}
				if count >= limit {
					truncated = true
					return nil
				}
			}
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return e
			}
		}
	}
	e := scan(root, 1)
	return count, truncated, e
}
func rootFor(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	if redact.Forbidden(p) {
		return "", fs.ErrPermission
	}
	st, e := os.Stat(p)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", errors.New("project root is not a directory")
	}
	original := p
	for {
		if _, e := os.Lstat(filepath.Join(p, ".git")); e == nil {
			return p, nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return original, nil
}
func (i *Inspector) Inspect(ctx context.Context, path string) Report {
	ctx, cancel := context.WithTimeout(ctx, host.Budget)
	defer cancel()
	out := Report{}
	root, e := rootFor(path)
	out["root"] = fact(root, "filesystem", e)
	if e != nil {
		return out
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".env", ".ssh", ".aws"} {
		_, e := os.Lstat(filepath.Join(root, name))
		out[name] = fact(e == nil, "existence only", e)
	}
	for _, name := range Manifests {
		p := filepath.Join(root, name)
		b, e := read(p)
		if e != nil {
			out[name] = fact(nil, p, e)
			continue
		}
		v, e := parse(name, b)
		out[name] = fact(v, p, e)
	}
	out["readme"] = cache.New(nil, "README", 0, cache.Missing)
	for _, name := range []string{"README.md", "readme.md", "README.rst", "README", "README.txt"} {
		b, e := read(filepath.Join(root, name))
		if e != nil {
			if !os.IsNotExist(e) {
				out["readme"] = fact(nil, name, e)
			}
			continue
		}
		title := ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) != "" {
				title = strings.TrimSpace(strings.TrimLeft(line, "# "))
				break
			}
		}
		out["readme"] = fact(title, name, nil)
		break
	}
	counts := map[string]int{}
	total := 0
	n, truncated, scanErr := walk(ctx, root, MaxDepth, MaxFiles, func(p string, d fs.DirEntry, depth int) bool {
		if redact.Forbidden(p) {
			key, _ := redact.Text("sensitive." + filepath.ToSlash(strings.TrimPrefix(p, root+string(os.PathSeparator))))
			out[key] = fact(true, "existence only", nil)
			return false
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return true
		}
		if lang := languages[strings.ToLower(filepath.Ext(p))]; lang != "" {
			counts[lang]++
			total++
		}
		return true
	})
	ratios := map[string]float64{}
	for k, n := range counts {
		ratios[k] = float64(n) * 100 / float64(total)
	}
	out["languages"] = fact(map[string]any{"percent_by_file_count": ratios, "files": total}, "bounded extension scan", scanErr)
	out["scan"] = fact(map[string]any{"entries": n, "truncated": truncated, "max_depth": MaxDepth, "max_entries": MaxFiles}, "filesystem", scanErr)
	i.git(ctx, root, out)
	return out
}

var languages = map[string]string{".go": "Go", ".java": "Java", ".kt": "Kotlin", ".js": "JavaScript", ".jsx": "JavaScript", ".ts": "TypeScript", ".tsx": "TypeScript", ".py": "Python", ".rs": "Rust", ".rb": "Ruby", ".php": "PHP", ".cs": "C#", ".c": "C", ".h": "C", ".cpp": "C++", ".swift": "Swift", ".sh": "Shell", ".lua": "Lua", ".scala": "Scala", ".vue": "Vue", ".svelte": "Svelte", ".dart": "Dart"}
