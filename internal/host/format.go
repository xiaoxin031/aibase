package host

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/xiaoxin031/aibase/internal/cache"
)

var versionNumber = regexp.MustCompile(`\d+(?:\.\d+)+(?:[-+._a-zA-Z0-9]*)?`)

// toolVersion takes the first version-looking token near the top; some tools
// (gradle, java) print a banner or blank line before it.
func toolVersion(name, s string) string {
	return versionNumber.FindString(firstLines(s, 5))
}
func sensitivePaths(platform string) []string {
	p := []string{".ssh", ".aws/credentials", ".azure", ".env"}
	switch platform {
	case "darwin":
		return append(p, ".config/gcloud", "Library/Keychains", "Library/Application Support/Google/Chrome", "Library/Application Support/Firefox")
	case "windows":
		return append(p, "AppData/Roaming/gcloud", "AppData/Local/Google/Chrome", "AppData/Local/Microsoft/Edge", "AppData/Roaming/Mozilla/Firefox", "AppData/Local/Microsoft/Credentials", "AppData/Roaming/Microsoft/Credentials")
	default:
		return append(p, ".config/gcloud", ".mozilla/firefox", ".config/google-chrome", ".config/chromium")
	}
}
func (i *Inspector) stub(ctx context.Context, name, path string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	if name == "java" && path == "/usr/bin/java" {
		if _, e := i.Runner.Run(ctx, "", "/usr/libexec/java_home"); e != nil {
			return "stub, no JDK"
		}
	}
	if name == "python3" && path == "/usr/bin/python3" {
		if _, e := i.Runner.Run(ctx, "", "/usr/bin/xcode-select", "-p"); e != nil {
			return "stub, no Xcode Command Line Tools"
		}
		b, e := i.Runner.Run(ctx, "", "/usr/bin/xcrun", "--find", "python3")
		if e != nil || trim(b) == "/usr/bin/python3" {
			return "stub, no Xcode Python"
		}
	}
	return ""
}
func normalize(name, s string) (any, error) {
	switch name {
	case "listeners", "services", "resources":
		return runtimeNormalize(name, s)
	case "memory_total":
		f := strings.Fields(s)
		if len(f) == 0 {
			break
		}
		n, e := strconv.ParseFloat(f[0], 64)
		if e != nil {
			break
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		return n / 1e9, nil
	case "disk":
		if strings.HasPrefix(s, "{") {
			var d struct {
				DeviceID        string
				FreeSpace, Size float64
			}
			if json.Unmarshal([]byte(s), &d) == nil && d.Size > 0 {
				return map[string]any{"volume": d.DeviceID, "used_gb": (d.Size - d.FreeSpace) / 1e9, "total_gb": d.Size / 1e9, "free_gb": d.FreeSpace / 1e9}, nil
			}
		}
		lines := strings.Split(s, "\n")
		if len(lines) > 1 {
			f := strings.Fields(lines[len(lines)-1])
			if len(f) >= 6 {
				total, e1 := strconv.ParseFloat(f[1], 64)
				used, e2 := strconv.ParseFloat(f[2], 64)
				free, e3 := strconv.ParseFloat(f[3], 64)
				if e1 == nil && e2 == nil && e3 == nil {
					return map[string]any{"volume": strings.Join(f[5:], " "), "used_gb": used * 1024 / 1e9, "total_gb": total * 1024 / 1e9, "free_gb": free * 1024 / 1e9}, nil
				}
			}
		}
	case "gpu":
		var v any
		if json.Unmarshal([]byte(s), &v) != nil {
			break
		}
		names := []string{}
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for _, k := range []string{"sppci_model", "Name"} {
					if n, ok := x[k].(string); ok && n != "" {
						names = append(names, n)
					}
				}
				for _, v := range x {
					walk(v)
				}
			case []any:
				for _, v := range x {
					walk(v)
				}
			}
		}
		walk(v)
		return names, nil
	case "cpu_model":
		var d struct{ ProcessorNameString string }
		if json.Unmarshal([]byte(s), &d) == nil && d.ProcessorNameString != "" {
			return strings.TrimSpace(d.ProcessorNameString), nil
		}
	case "os_version":
		var d struct{ ProductName, DisplayVersion, CurrentBuild string }
		if json.Unmarshal([]byte(s), &d) == nil && d.ProductName != "" {
			return strings.TrimSpace(d.ProductName + " " + d.DisplayVersion + " " + d.CurrentBuild), nil
		}
	case "admin":
		return strings.EqualFold(s, "true"), nil
	}
	if name == "disk" || name == "memory_total" || name == "gpu" {
		return nil, fmt.Errorf("unrecognized %s output", name)
	}
	return s, nil
}
func value(r Report, k string) string {
	f, ok := r[k]
	if !ok || f.Status != cache.OK || f.Value == nil {
		return ""
	}
	return fmt.Sprint(f.Value)
}

// Summary uses only the supplied snapshot and never probes the machine.
func Summary(r Report) string {
	parts := []string{}
	add := func(label, v string) {
		if v != "" {
			parts = append(parts, label+v)
		}
	}
	osName := value(r, "os")
	if v := value(r, "os_version"); v != "" {
		osName = v
		if value(r, "os") == "darwin" {
			osName = "macOS " + v
		}
	}
	add("", strings.TrimSpace(osName+"/"+value(r, "architecture")))
	cpu := value(r, "cpu_model")
	if n := value(r, "cpu_cores"); n != "" && cpu != "" {
		cpu += " (" + n + " cores)"
	} else if n != "" {
		cpu = n + " cores"
	}
	add("CPU ", strings.TrimSpace(cpu))
	if n, ok := r["memory_total"].Value.(float64); ok && r["memory_total"].Status == cache.OK {
		add("RAM ", fmt.Sprintf("%.1fGB", n))
	}
	if d, ok := r["disk"].Value.(map[string]any); ok && r["disk"].Status == cache.OK {
		add("disk free ", fmt.Sprintf("%.1fGB", d["free_gb"]))
	}
	if g, ok := r["gpu"].Value.([]any); ok && r["gpu"].Status == cache.OK {
		names := []string{}
		for _, n := range g {
			names = append(names, fmt.Sprint(n))
		}
		add("GPU ", strings.Join(names, ", "))
	}
	add("shell ", value(r, "shell"))
	for _, k := range []string{"ssh", "container", "wsl", "root", "admin"} {
		if value(r, k) == "true" && (k != "admin" || value(r, "root") != "true") {
			add("", k)
		}
	}
	for _, n := range Tools {
		f := r["tool."+n]
		if f.Status == cache.OK {
			if m, ok := f.Value.(map[string]any); ok {
				add("", strings.TrimSpace(n+" "+fmt.Sprint(m["version"])))
			}
		}
	}
	return strings.Join(parts, "; ")
}
func Text(r Report) string {
	lines := []string{Summary(r)}
	keys := []string{}
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := r[k]
		if strings.HasPrefix(k, "sensitive.") {
			if f.Status == cache.OK && f.Value == true {
				lines = append(lines, strings.TrimPrefix(k, "sensitive.")+": exists (contents not read)")
			}
			continue
		}
		if k == "disk" && f.Status == cache.OK {
			d, _ := f.Value.(map[string]any)
			lines = append(lines, fmt.Sprintf("disk %v: %.1f / %.1f GB used; %.1f GB free", d["volume"], d["used_gb"], d["total_gb"], d["free_gb"]))
		}
		switch k {
		case "listeners", "services", "containers", "resources":
			if f.Status == cache.OK {
				lines = append(lines, runtimeText(k, f.Value)...)
			}
		}
		if f.Status == cache.Timeout || f.Status == cache.Denied || f.Status == cache.Stale || ((k == "listeners" || k == "services" || k == "resources" || k == "containers") && f.Status != cache.OK) {
			line := k + ": " + string(f.Status)
			if m, ok := f.Value.(map[string]any); ok && m["reason"] != nil {
				line += " (" + fmt.Sprint(m["reason"]) + ")"
			}
			lines = append(lines, line)
		} else if f.Status != cache.OK && f.Status != cache.Missing {
			if m, ok := f.Value.(map[string]any); ok && m["reason"] != nil {
				lines = append(lines, k+": "+string(f.Status)+" ("+fmt.Sprint(m["reason"])+")")
			}
		}
	}
	return strings.Join(lines, "\n")
}
