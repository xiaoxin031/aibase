// Package doctor performs bounded read-only checks; repair strings are advice.
package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/clients"
	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

func Run(dir string) []Check { return run(dir, false) }

// Quick excludes cache scanning, skill discovery and secret scanning.
func Quick(dir string) []Check { return run(dir, true) }

func run(dir string, quick bool) []Check {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	out := []Check{}
	add := func(n, s, d, f string) {
		n, _ = redact.Text(n)
		d, _ = redact.Text(d)
		f, _ = redact.Text(f)
		out = append(out, Check{n, s, d, f})
	}
	for _, d := range clients.Detect() {
		status, detail := "ok", "not detected; optional client"
		if d.Detected {
			status, detail = "warn", "detected; integration missing"
		}
		if d.Configured {
			status, detail = "ok", "configuration present; activation unverified"
		}
		fix := ""
		if d.Detected && !d.Configured {
			fix = "aibase install " + d.Name + " --dry-run"
		}
		if d.Name == "codex" && d.Configured {
			switch clients.CodexHookTrust() {
			case "trusted":
				detail = "hooks trusted and enabled in Codex"
			case "untrusted":
				status, detail = "warn", "hooks not trusted in Codex"
				fix = "aibase install codex"
			default:
				status, detail = "warn", "configuration present; activation/trust unverified"
				fix = "In Codex: /hooks — trust and enable aibase"
			}
		}
		if d.Error != "" {
			status = "fail"
			detail = d.Error
		}
		add("client."+d.Name, status, detail, fix)
	}
	if p, e := exec.LookPath("aibase"); e == nil {
		add("path", "ok", p, "")
	} else {
		add("path", "fail", "The aibase executable directory is missing from PATH. Add it persistently, then reopen your terminal.", pathAdvice(runtime.GOOS, os.Getenv("SHELL"), executableDir()))
	}
	home := cache.Home()
	if st, e := os.Stat(home); e != nil {
		add("permissions", "warn", "data directory unavailable", "aibase context")
	} else if runtime.GOOS == "windows" {
		add("permissions", "ok", "per-user profile directory (ACL inherited)", `icacls "`+home+`"`)
	} else if st.Mode().Perm()&0077 != 0 {
		add("permissions", "fail", "data directory accessible to other users", "chmod 700 "+shellQuote(home))
	} else {
		add("permissions", "ok", "directory restricted", "")
	}
	for _, u := range memory.Usage(dir) {
		status, detail := "ok", fmt.Sprintf("%d/%d characters", u.Used, u.Limit)
		if u.Error != "" {
			status = "warn"
			detail = u.Error
		} else if u.Used*10 >= u.Limit*9 {
			status = "warn"
		}
		add("memory."+u.Target, status, detail, "aibase ls "+u.Target)
	}
	if !quick {
		// Read at most 256 cache entries; unknown/oversized files are warned, not trusted.
		if f, e := skills.OpenDir(filepath.Join(home, "cache")); e == nil {
			ds, err := f.ReadDir(257)
			f.Close()
			if err != nil && err != io.EOF {
				add("cache", "warn", err.Error(), "aibase inspect host")
			}
			if len(ds) > 256 {
				add("cache", "warn", "cache check truncated at 256 entries", "aibase inspect host")
				ds = ds[:256]
			}
			for _, d := range ds {
				if !strings.HasSuffix(d.Name(), ".json") {
					continue
				}
				b, e := skills.Read(filepath.Join(home, "cache", d.Name()))
				var v cache.Fact
				if e != nil || json.Unmarshal(b, &v) != nil || v.At.IsZero() || v.TTL < 0 {
					add("cache", "warn", "damaged or oversized: "+d.Name(), "aibase inspect host --level tools")
				} else if !v.Fresh(time.Now()) {
					add("cache", "warn", "expired: "+d.Name(), "aibase inspect host --level tools")
				}
			}
		} else {
			add("cache", "warn", "no readable fact cache", "aibase inspect host --level basic")
		}
	}
	needs := map[string][]string{"pom.xml": {"java", "mvn"}, "build.gradle": {"java", "gradle"}, "build.gradle.kts": {"java", "gradle"}, "go.mod": {"go"}, "package.json": {"node", "npm"}, "Cargo.toml": {"cargo", "rustc"}, "pyproject.toml": {"python3"}, "Dockerfile": {"docker"}}
	for _, n := range []string{"pom.xml", "build.gradle", "build.gradle.kts", "go.mod", "package.json", "Cargo.toml", "pyproject.toml", "Dockerfile"} {
		if st, e := os.Lstat(filepath.Join(dir, n)); e == nil && st.Mode().IsRegular() {
			for _, tool := range needs[n] {
				if _, e := exec.LookPath(tool); e != nil {
					add("project."+tool, "warn", n+" declares a toolchain; "+tool+" missing", installCommand(tool))
				}
			}
		}
	}
	if quick {
		return out
	}
	all, e := skills.List(false)
	if e != nil {
		add("skills", "warn", e.Error(), "aibase skill check")
	} else {
		for _, s := range all {
			if s.Error != "" {
				add("skill."+s.Name, "fail", s.Error, "aibase skill show "+s.Name)
			} else if len(s.Missing) > 0 {
				add("skill."+s.Name, "warn", "missing: "+strings.Join(s.Missing, ", "), "aibase skill check "+s.Name)
			}
		}
	}
	out = append(out, dataChecks(home)...)
	return out
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func Text(cs []Check) string {
	var b strings.Builder
	for _, c := range cs {
		symbol := map[string]string{"ok": "✓", "fail": "✗", "warn": "!"}[c.Status]
		fmt.Fprintf(&b, "%s %s: %s", symbol, c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(&b, " — %s", c.Fix)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func executableDir() string { p, _ := os.Executable(); return filepath.Dir(p) }
func installCommand(tool string) string {
	packages := map[string]map[string]string{
		"brew":    {"java": "openjdk", "mvn": "maven", "gradle": "gradle", "go": "go", "node": "node", "npm": "node", "cargo": "rust", "rustc": "rust", "python3": "python", "docker": "docker"},
		"apt-get": {"java": "default-jdk", "mvn": "maven", "gradle": "gradle", "go": "golang-go", "node": "nodejs", "npm": "npm", "cargo": "cargo", "rustc": "rustc", "python3": "python3", "docker": "docker.io"},
		"apk":     {"java": "openjdk21", "mvn": "maven", "gradle": "gradle", "go": "go", "node": "nodejs", "npm": "npm", "cargo": "cargo", "rustc": "rust", "python3": "python3", "docker": "docker-cli"},
	}
	for _, manager := range []string{"brew", "apt-get", "apk"} {
		if _, e := exec.LookPath(manager); e == nil {
			if pkg := packages[manager][tool]; pkg != "" {
				prefix := ""
				if manager != "brew" && os.Geteuid() != 0 {
					prefix = "sudo "
				}
				verb := " install "
				if manager == "apk" {
					verb = " add "
				}
				return prefix + manager + verb + pkg
			}
		}
	}
	if runtime.GOOS == "windows" {
		return "Get-Command " + tool + " -ErrorAction SilentlyContinue"
	}
	return "command -v " + shellQuote(tool)
}

func pathAdvice(goos, shell, dir string) string {
	if goos == "windows" {
		q := strings.ReplaceAll(dir, "'", "''")
		return "$dir = '" + q + "'; $p = [string][Environment]::GetEnvironmentVariable('Path', 'User'); if (@($p -split ';') -notcontains $dir) { [Environment]::SetEnvironmentVariable('Path', (($p.TrimEnd(';') + ';' + $dir).TrimStart(';')), 'User') }"
	}
	h, _ := os.UserHomeDir()
	rc := filepath.Join(h, ".profile")
	line := "export PATH=" + shellQuote(dir) + ":\"$PATH\""
	switch filepath.Base(shell) {
	case "zsh":
		base := os.Getenv("ZDOTDIR")
		if base == "" {
			base = h
		}
		rc = filepath.Join(base, ".zshrc")
	case "bash":
		rc = filepath.Join(h, ".bashrc")
		if goos == "darwin" {
			rc = filepath.Join(h, ".bash_profile")
		}
	case "fish":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(h, ".config")
		}
		rc = filepath.Join(base, "fish", "config.fish")
		line = "fish_add_path " + shellQuote(dir)
	}
	return "Add this line to " + rc + ": " + line
}
