//go:build darwin

package host

import (
	"context"
	"os"
	"strings"
	"time"
)

func (i *Inspector) platform() []probe {
	d := 24 * time.Hour
	return []probe{
		{name: "root", source: "geteuid", run: func(context.Context) (any, error) { return os.Geteuid() == 0, nil }},
		i.command("os_version", d, "sw_vers", "-productVersion"), i.command("kernel", d, "sysctl", "-n", "kern.osrelease"), i.command("hostname", 0, "sysctl", "-n", "kern.hostname"),
		i.command("cpu_model", d, "sysctl", "-n", "machdep.cpu.brand_string"), i.command("memory_total", d, "sysctl", "-n", "hw.memsize"),
		i.command("disk", time.Minute, "df", "-Pk", "/System/Volumes/Data"), i.command("gpu", 7*d, "system_profiler", "SPDisplaysDataType", "-json"), i.command("vm", d, "sysctl", "-n", "kern.hv_vmm_present"),
		{name: "admin", source: "geteuid + id -Gn", run: func(ctx context.Context) (any, error) {
			if os.Geteuid() == 0 {
				return true, nil
			}
			b, e := i.Runner.Run(ctx, "", "id", "-Gn")
			if e != nil {
				return nil, e
			}
			return strings.Contains(" "+trim(b)+" ", " admin "), nil
		}},
	}
}
func (i *Inspector) runtime() []probe {
	return []probe{
		i.command("listeners", time.Minute, "lsof", "-nP", "-FpcPn", "-iTCP", "-sTCP:LISTEN"), i.command("services", time.Minute, "launchctl", "list"),
		i.containers(),
		i.command("resources", time.Minute, "ps", "-Ac", "-o", "pid,pcpu,pmem,comm", "-r"),
	}
}
