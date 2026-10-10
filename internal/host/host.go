// Package host observes L0 identity/toolchains and opt-in L1 runtime state.
// Inspect has one shared 1.5s deadline and at most eight active probes. Missing
// privileges/platform support remain explicit facts. No installation is performed.
package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
)

type Report map[string]cache.Fact
type Inspector struct {
	Runner Runner
	FS     FS
	Cache  *cache.Store
	Env    func(string) string
	Home   string
	// Budget bounds one inspection; zero means DefaultBudget.
	Budget time.Duration
}

func New() *Inspector {
	h, _ := os.UserHomeDir()
	return &Inspector{Runner: ExecRunner{}, FS: OSFS{}, Cache: cache.NewStore(""), Env: os.Getenv, Home: h}
}

type probe struct {
	name, source, key string
	ttl               time.Duration
	run               func(context.Context) (any, error)
}

func (i *Inspector) command(name string, ttl time.Duration, cmd string, args ...string) probe {
	return probe{name: name, source: cmd + " " + strings.Join(args, " "), ttl: ttl, run: func(ctx context.Context) (any, error) {
		b, e := i.Runner.Run(ctx, "", cmd, args...)
		s := trim(b)
		if len(b) >= MaxOutput && e == nil && name == "resources" && cmd == "ps" &&
			(strings.Join(args, " ") == "-Ac -o pid,pcpu,pmem,comm -r" || strings.Join(args, " ") == "-eo pid,pcpu,pmem,comm --sort=-pcpu") {
			// These fixed ps invocations are CPU-sorted; only complete leading rows are needed.
			if end := strings.LastIndexByte(string(b), '\n'); end >= 0 {
				if v, err := runtimeNormalize(name, string(b[:end])); err == nil && len(v.([]Resource)) == 10 {
					return v, nil
				}
			}
		}
		if len(b) >= MaxOutput && (name == "listeners" || name == "services" || name == "resources") {
			return cache.New(map[string]string{"reason": "runtime output reached the 64 KiB read limit; result unknown"}, cmd, ttl, cache.Unsupported), nil
		}
		if e != nil && (strings.Contains(strings.ToLower(s), "operation not permitted") || strings.Contains(strings.ToLower(s), "permission denied")) {
			return nil, fs.ErrPermission
		}
		if e == nil {
			return normalize(name, s)
		}
		return s, e
	}}
}
func (i *Inspector) file(name, path string, ttl time.Duration, parse func(string) any) probe {
	return probe{name: name, source: path, ttl: ttl, run: func(ctx context.Context) (any, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		b, e := i.FS.ReadFile(path)
		if e != nil {
			return nil, e
		}
		v := parse(string(b))
		if v == "" {
			return nil, errors.New("unrecognized platform metadata")
		}
		return v, nil
	}}
}
func (i *Inspector) Inspect(ctx context.Context, level int) Report {
	return i.inspect(ctx, level, true)
}

// InspectLevel keeps basic free of tool version probes.
func (i *Inspector) InspectLevel(ctx context.Context, level string) (Report, error) {
	switch level {
	case "basic":
		return i.inspect(ctx, 0, false), nil
	case "tools":
		return i.inspect(ctx, 0, true), nil
	case "runtime":
		return i.inspect(ctx, 1, true), nil
	default:
		return nil, fmt.Errorf("level must be basic, tools or runtime")
	}
}
func (i *Inspector) inspect(ctx context.Context, level int, tools bool) Report {
	budget := i.Budget
	if budget <= 0 {
		budget = DefaultBudget()
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out := Report{}
	live := func(k string, v any) { out[k] = cache.New(v, "session", 0, cache.OK) }
	live("os", runtime.GOOS)
	live("architecture", runtime.GOARCH)
	live("cpu_cores", runtime.NumCPU())
	for k, env := range map[string]string{"user": "USER", "shell": "SHELL", "terminal": "TERM", "language": "LANG"} {
		v := i.Env(env)
		if v == "" && k == "user" {
			v = i.Env("USERNAME")
		}
		if v == "" && k == "shell" {
			v = i.Env("COMSPEC")
		}
		if v == "" && k == "language" {
			v = i.Env("LC_ALL")
		}
		live(k, v)
		if v == "" {
			f := out[k]
			f.Status = cache.Missing
			out[k] = f
		}
	}
	live("ssh", i.Env("SSH_CONNECTION") != "" || i.Env("SSH_CLIENT") != "" || i.Env("SSH_TTY") != "")
	zone, offset := time.Now().Zone()
	live("timezone", map[string]any{"name": zone, "offset_seconds": offset})
	for _, k := range []string{"os_version", "kernel", "hostname", "admin", "container", "wsl", "vm", "cpu_model", "memory_total", "disk", "gpu"} {
		out[k] = cache.New(nil, "platform", 24*time.Hour, cache.Unsupported)
	}
	tasks := i.platform()
	// Session-dependent privilege and container facts are deliberately not cached.
	for n := range tasks {
		if tasks[n].ttl > 0 {
			tasks[n].key = "host:v2:" + runtime.GOOS + ":" + tasks[n].name
		}
	}
	toolNames := Tools
	if !tools {
		toolNames = nil
	}
	for _, name := range toolNames {
		name := name
		tasks = append(tasks, probe{name: "tool." + name, source: "PATH:" + name, ttl: 24 * time.Hour, run: func(ctx context.Context) (any, error) {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			p, e := i.Runner.LookPath(name)
			if e != nil {
				return nil, e
			}
			st, e := i.FS.Stat(p)
			if e != nil {
				return nil, e
			}
			key := fmt.Sprintf("tool:v2:%s:%s:%d:%d", name, p, st.ModTime().UnixNano(), st.Size())
			if i.Cache != nil {
				if f, ok := i.Cache.Get(key); ok && f.Fresh(time.Now()) && f.Status == cache.OK {
					return f, nil
				}
			}
			if reason := i.stub(ctx, name, p); reason != "" {
				return cache.New(map[string]any{"path": p, "reason": reason}, p, 24*time.Hour, cache.Missing), nil
			}
			args := versionArgs(name)
			b, e := i.Runner.Run(ctx, "", p, args...)
			if e != nil {
				return nil, e
			}
			f := cache.New(map[string]any{"path": p, "version": toolVersion(name, trim(b))}, p, 24*time.Hour, cache.OK)
			if ctx.Err() == nil && i.Cache != nil {
				_ = i.Cache.Put(key, f)
			}
			return f, nil
		}})
	}
	for _, p := range sensitivePaths(runtime.GOOS) {
		p := p
		tasks = append(tasks, probe{name: "sensitive." + p, source: "existence only", run: func(context.Context) (any, error) { _, e := i.FS.Stat(filepath.Join(i.Home, p)); return e == nil, e }})
	}
	if level >= 1 {
		for _, p := range i.runtime() {
			p.key = "runtime:v2:" + runtime.GOOS + ":" + p.name
			tasks = append([]probe{p}, tasks...)
		}
	}
	type result struct {
		name string
		f    cache.Fact
	}
	done := make(chan result, len(tasks))
	jobs := make(chan probe, len(tasks))
	pending := map[string]probe{}
	for _, p := range tasks {
		if p.key != "" && i.Cache != nil {
			if f, ok := i.Cache.Get(p.key); ok && f.Fresh(time.Now()) {
				out[p.name] = f
				continue
			}
		}
		pending[p.name] = p
		jobs <- p
	}
	close(jobs)
	for n := 0; n < min(8, len(pending)); n++ {
		go func() {
			for p := range jobs {
				if ctx.Err() != nil {
					return
				}
				v, e := p.run(ctx)
				f, ok := v.(cache.Fact)
				if !ok {
					f = cache.New(v, p.source, p.ttl, Status(e))
				}
				if e != nil {
					f.Value = nil
				}
				if ctx.Err() != nil {
					f = cache.New(nil, p.source, p.ttl, cache.Timeout)
				}
				if p.key != "" && i.Cache != nil && f.Status == cache.OK && ctx.Err() == nil {
					_ = i.Cache.Put(p.key, f)
				}
				done <- result{p.name, f}
			}
		}()
	}
	for len(pending) > 0 {
		select {
		case r := <-done:
			out[r.name] = r.f
			delete(pending, r.name)
		case <-ctx.Done():
			for k, p := range pending {
				out[k] = cache.New(nil, p.source, p.ttl, cache.Timeout)
			}
			return out
		}
	}
	return out
}
func firstLines(s string, n int) string {
	a := strings.Split(s, "\n")
	return strings.Join(a[:min(n, len(a))], "\n")
}

var Tools = []string{"brew", "apt", "dnf", "yum", "apk", "pacman", "winget", "scoop", "choco", "go", "java", "node", "python", "python3", "rustc", "cargo", "ruby", "php", "mvn", "gradle", "npm", "pnpm", "yarn", "bun", "deno", "uv", "pip", "dotnet", "git", "docker", "podman", "kubectl", "ffmpeg", "ffprobe", "magick", "convert", "yt-dlp", "sox", "exiftool", "claude", "codex", "gemini", "cursor-agent", "opencode", "qwen", "aider", "crush"}

func (i *Inspector) containers() probe {
	return probe{name: "containers", source: "docker ps (local daemon, first 100)", ttl: time.Minute, run: func(ctx context.Context) (any, error) {
		endpoint := "unix:///var/run/docker.sock"
		if runtime.GOOS == "windows" {
			endpoint = "npipe:////./pipe/docker_engine"
		} else if runtime.GOOS == "darwin" {
			p := filepath.Join(i.Home, ".docker", "run", "docker.sock")
			if _, err := i.FS.Stat(p); err == nil {
				endpoint = "unix://" + p
			}
		}
		b, e := i.Runner.Run(ctx, "", "docker", "--host", endpoint, "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}")
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if Status(e) == cache.Missing {
				return nil, e
			}
			return cache.New(map[string]string{"reason": "local Docker daemon unavailable or access denied"}, "docker ps", time.Minute, cache.Denied), nil
		}
		if len(b) >= MaxOutput {
			return cache.New(map[string]string{"reason": "Docker output reached the 64 KiB read limit; result unknown"}, "docker ps", time.Minute, cache.Unsupported), nil
		}
		rows := []Container{}
		for _, line := range strings.Split(strings.TrimRight(string(b), "\r\n"), "\n") {
			f := strings.Split(line, "\t")
			if len(f) == 4 {
				rows = append(rows, Container{f[0], f[1], f[2], f[3]})
			}
		}
		return rows, nil
	}}
}

func versionArgs(name string) []string {
	switch name {
	case "go":
		return []string{"version"}
	case "java", "ffmpeg", "ffprobe", "magick", "convert":
		return []string{"-version"}
	case "exiftool":
		return []string{"-ver"}
	case "kubectl":
		return []string{"version", "--client"}
	default:
		return []string{"--version"}
	}
}
