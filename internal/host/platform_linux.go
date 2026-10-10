//go:build linux

package host

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
)

func (i *Inspector) platform() []probe {
	day := 24 * time.Hour
	p := []probe{
		{name: "root", source: "geteuid", run: func(context.Context) (any, error) { return os.Geteuid() == 0, nil }},
		i.file("os_version", "/etc/os-release", day, func(s string) any {
			for _, l := range strings.Split(s, "\n") {
				if strings.HasPrefix(l, "PRETTY_NAME=") {
					return strings.Trim(strings.TrimPrefix(l, "PRETTY_NAME="), "\"")
				}
			}
			return ""
		}),
		i.file("kernel", "/proc/sys/kernel/osrelease", day, func(s string) any { return strings.TrimSpace(s) }),
		i.file("hostname", "/proc/sys/kernel/hostname", 0, func(s string) any { return strings.TrimSpace(s) }),
		i.file("cpu_model", "/proc/cpuinfo", day, func(s string) any {
			for _, l := range strings.Split(s, "\n") {
				k, v, ok := strings.Cut(l, ":")
				if ok && (strings.TrimSpace(k) == "model name" || strings.TrimSpace(k) == "Hardware") {
					return strings.TrimSpace(v)
				}
			}
			return ""
		}),
		i.file("memory_total", "/proc/meminfo", day, func(s string) any {
			for _, l := range strings.Split(s, "\n") {
				if strings.HasPrefix(l, "MemTotal:") {
					v, _ := normalize("memory_total", strings.TrimSpace(strings.TrimPrefix(l, "MemTotal:")))
					return v
				}
			}
			return ""
		}),
		i.command("disk", 60*time.Second, "df", "-Pk", "/"),
		{name: "vm", source: "/sys/class/dmi/id/product_name + /proc/cpuinfo", ttl: day, run: func(context.Context) (any, error) {
			b, e := i.FS.ReadFile("/sys/class/dmi/id/product_name")
			cpu, ce := i.FS.ReadFile("/proc/cpuinfo")
			s := strings.ToLower(string(b) + "\n" + string(cpu))
			for _, marker := range []string{"kvm", "qemu", "vmware", "virtualbox", "virtual machine", "xen", "hypervisor"} {
				if strings.Contains(s, marker) {
					return map[string]any{"detected": true, "evidence": marker}, nil
				}
			}
			if e != nil && ce != nil {
				return nil, e
			}
			return cache.New(map[string]any{"evidence": trim(b)}, "DMI/CPU: no conclusive VM marker", day, cache.Unsupported), nil
		}},
		{name: "admin", source: "geteuid", run: func(context.Context) (any, error) { return os.Geteuid() == 0, nil }},
		{name: "wsl", source: "/proc/sys/kernel/osrelease + environment", run: func(context.Context) (any, error) {
			if i.Env("WSL_DISTRO_NAME") != "" {
				return true, nil
			}
			b, e := i.FS.ReadFile("/proc/sys/kernel/osrelease")
			return strings.Contains(strings.ToLower(string(b)), "microsoft") || i.Env("WSL_DISTRO_NAME") != "", e
		}},
		{name: "container", source: "/.dockerenv + /run/.containerenv + /proc/1/cgroup", run: func(context.Context) (any, error) {
			for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
				if _, e := i.FS.Stat(p); e == nil {
					return true, nil
				}
			}
			b, e := i.FS.ReadFile("/proc/1/cgroup")
			s := string(b)
			return strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") || strings.Contains(s, "lxc"), e
		}},
		{name: "gpu", source: "/sys/class/drm", ttl: 7 * day, run: func(ctx context.Context) (any, error) {
			ds, e := i.FS.ReadDir("/sys/class/drm")
			if e != nil {
				return nil, e
			}
			out := []string{}
			for _, d := range ds {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if !strings.HasPrefix(d.Name(), "card") || strings.Contains(d.Name(), "-") {
					continue
				}
				b, e := i.FS.ReadFile("/sys/class/drm/" + d.Name() + "/device/product_name")
				if e == nil && trim(b) != "" {
					out = append(out, trim(b))
				}
			}
			if len(out) == 0 {
				b, e := i.Runner.Run(ctx, "", "lspci", "-mm")
				if e != nil {
					return cache.New(nil, "GPU name unavailable", 7*day, cache.Unsupported), nil
				}
				for _, line := range strings.Split(trim(b), "\n") {
					fields := strings.Split(line, "\"")
					if len(fields) >= 6 && (strings.Contains(fields[1], "VGA") || strings.Contains(fields[1], "3D") || strings.Contains(fields[1], "Display")) {
						out = append(out, fields[3]+" "+fields[5])
					}
				}
			}
			return out, nil
		}},
	}
	return p
}
func (i *Inspector) runtime() []probe {
	return []probe{
		{name: "listeners", source: "/proc/net/tcp* + /proc/*/fd", ttl: time.Minute, run: i.normalizedLinuxListeners},
		i.command("services", time.Minute, "systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--no-legend"),
		i.containers(),
		i.command("resources", time.Minute, "ps", "-eo", "pid,pcpu,pmem,comm", "--sort=-pcpu"),
	}
}
