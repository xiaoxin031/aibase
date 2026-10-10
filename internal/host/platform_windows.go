//go:build windows

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx     = kernel32.NewProc("GlobalMemoryStatusEx")
	getDiskFreeSpaceEx       = kernel32.NewProc("GetDiskFreeSpaceExW")
	getUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

const (
	ntCurrentVersion = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	displayClass     = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`
	tokenElevation   = 20
)

func (i *Inspector) ps(name string, ttl time.Duration, script string) probe {
	return i.command(name, ttl, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); $OutputEncoding = [Console]::OutputEncoding; `+script)
}

func native(name, source string, ttl time.Duration, run func() (any, error)) probe {
	return probe{name: name, source: source, ttl: ttl, run: func(ctx context.Context) (any, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		return run()
	}}
}

// Basic facts come from the registry and kernel32: no process start-up, so they
// stay within budget on a cold machine where PowerShell alone takes seconds.
func (i *Inspector) platform() []probe {
	d := 24 * time.Hour
	return []probe{
		native("os_version", `registry `+ntCurrentVersion, d, osVersion),
		native("kernel", `registry `+ntCurrentVersion, d, kernelVersion),
		native("hostname", "os.Hostname", 0, func() (any, error) { return os.Hostname() }),
		native("admin", "token elevation", 0, elevated),
		native("cpu_model", `registry HARDWARE\DESCRIPTION\System\CentralProcessor\0`, d, func() (any, error) {
			s, e := regString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString")
			return strings.Join(strings.Fields(s), " "), e
		}),
		native("memory_total", "GlobalMemoryStatusEx", d, memoryTotal),
		native("disk", "GetDiskFreeSpaceExW", time.Minute, systemDisk),
		native("gpu", `registry `+displayClass, 7*d, gpus),
		native("vm", `registry HARDWARE\DESCRIPTION\System\BIOS`, d, func() (any, error) {
			m, e1 := regString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemManufacturer")
			p, e2 := regString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName")
			if e1 != nil && e2 != nil {
				return nil, e1
			}
			return strings.TrimSpace(m + " " + p), nil
		}),
		native("language", "GetUserDefaultLocaleName", 0, locale),
	}
}

func (i *Inspector) runtime() []probe {
	return []probe{
		i.ps("listeners", time.Minute, `$names=@{}; Get-Process | ForEach-Object { $names[$_.Id]=$_.ProcessName }; @(Get-NetTCPConnection -State Listen -ErrorAction Stop | ForEach-Object { $n=$names[[int]$_.OwningProcess]; if (!$n) {$n='unknown'}; [pscustomobject]@{port=$_.LocalPort;proto='tcp';bind=$_.LocalAddress;process=$n;pid=$_.OwningProcess} }) | ConvertTo-Json -Compress`),
		i.ps("services", time.Minute, `Get-CimInstance Win32_Service -Filter "State='Running'" -ErrorAction Stop | ForEach-Object { $exe=$null; if ($_.PathName -match '^"([^"]+)"') {$exe=$matches[1]} elseif ($_.PathName -match '^(.*?\.exe)(?:\s|$)') {$exe=$matches[1]}; $vendor=$null; if ($exe) { $vendor=(Get-Item -LiteralPath $exe -ErrorAction SilentlyContinue).VersionInfo.CompanyName }; if (!$vendor -or $vendor -notmatch 'Microsoft') {$_.Name} }`),
		i.containers(),
		i.ps("resources", time.Minute, `$names=@{}; Get-Process | ForEach-Object { $names[$_.Id]=$_.ProcessName }; $total=(Get-CimInstance Win32_ComputerSystem -ErrorAction Stop).TotalPhysicalMemory; @(Get-CimInstance Win32_PerfFormattedData_PerfProc_Process -ErrorAction Stop | Where-Object {$_.IDProcess -gt 0} | Sort-Object PercentProcessorTime -Descending | Select-Object -First 10 | ForEach-Object { [pscustomobject]@{pid=$_.IDProcess;cpu=[double]$_.PercentProcessorTime;mem=100.0*$_.WorkingSetPrivate/$total;name=$(if ($names[[int]$_.IDProcess]) {$names[[int]$_.IDProcess]} else {$_.Name -replace '#\d+$',''})} }) | ConvertTo-Json -Compress`),
	}
}

func osVersion() (any, error) {
	name, e := regString(ntCurrentVersion, "ProductName")
	if e != nil {
		return nil, e
	}
	build, _ := regString(ntCurrentVersion, "CurrentBuild")
	display, _ := regString(ntCurrentVersion, "DisplayVersion")
	// Windows 11 still reports "Windows 10" in ProductName; build 22000 is the boundary.
	if n, _ := strconv.Atoi(build); n >= 22000 {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	return strings.Join(strings.Fields(name+" "+display+" "+build), " "), nil
}

func kernelVersion() (any, error) {
	major, e := regDWORD(ntCurrentVersion, "CurrentMajorVersionNumber")
	if e != nil {
		return nil, e
	}
	minor, _ := regDWORD(ntCurrentVersion, "CurrentMinorVersionNumber")
	build, _ := regString(ntCurrentVersion, "CurrentBuild")
	v := fmt.Sprintf("%d.%d.%s", major, minor, build)
	if ubr, e := regDWORD(ntCurrentVersion, "UBR"); e == nil {
		v += fmt.Sprintf(".%d", ubr)
	}
	return v, nil
}

func elevated() (any, error) {
	p, e := syscall.GetCurrentProcess()
	if e != nil {
		return nil, e
	}
	var t syscall.Token
	if e = syscall.OpenProcessToken(p, syscall.TOKEN_QUERY, &t); e != nil {
		return nil, e
	}
	defer t.Close()
	var v, n uint32
	if e = syscall.GetTokenInformation(t, tokenElevation, (*byte)(unsafe.Pointer(&v)), 4, &n); e != nil {
		return nil, e
	}
	return v != 0, nil
}

func memoryTotal() (any, error) {
	var m struct {
		Length, MemoryLoad                        uint32
		TotalPhys, AvailPhys, TotalPageFile       uint64
		AvailPageFile, TotalVirtual, AvailVirtual uint64
		AvailExtendedVirtual                      uint64
	}
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, e := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return nil, e
	}
	return float64(m.TotalPhys) / 1e9, nil
}

func systemDisk() (any, error) {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	p, e := syscall.UTF16PtrFromString(drive + `\`)
	if e != nil {
		return nil, e
	}
	var avail, total, free uint64
	if r, _, e := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free))); r == 0 {
		return nil, e
	}
	if total == 0 {
		return nil, errors.New("system drive reports zero size")
	}
	return map[string]any{"volume": drive, "used_gb": float64(total-free) / 1e9, "total_gb": float64(total) / 1e9, "free_gb": float64(avail) / 1e9}, nil
}

func gpus() (any, error) {
	var k syscall.Handle
	if e := regOpen(displayClass, &k); e != nil {
		return nil, e
	}
	defer syscall.RegCloseKey(k)
	names := []string{}
	for n := uint32(0); n < 32; n++ {
		buf := make([]uint16, 256)
		size := uint32(len(buf))
		if syscall.RegEnumKeyEx(k, n, &buf[0], &size, nil, nil, nil, nil) != nil {
			break
		}
		sub := syscall.UTF16ToString(buf[:size])
		if len(sub) != 4 || strings.Trim(sub, "0123456789") != "" {
			continue
		}
		if s, e := regString(displayClass+`\`+sub, "DriverDesc"); e == nil && s != "" && !slices.Contains(names, s) {
			names = append(names, s)
		}
	}
	return names, nil
}

func locale() (any, error) {
	buf := make([]uint16, 85)
	if r, _, e := getUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r == 0 {
		return nil, e
	}
	return syscall.UTF16ToString(buf), nil
}

func regOpen(path string, k *syscall.Handle) error {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	return syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, syscall.KEY_READ, k)
}

func regValue(path, name string) (uint32, []byte, error) {
	var k syscall.Handle
	if e := regOpen(path, &k); e != nil {
		return 0, nil, e
	}
	defer syscall.RegCloseKey(k)
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return 0, nil, e
	}
	var typ, size uint32
	if e = syscall.RegQueryValueEx(k, n, nil, &typ, nil, &size); e != nil {
		return 0, nil, e
	}
	if size == 0 || size > 64<<10 {
		return typ, nil, nil
	}
	buf := make([]byte, size)
	if e = syscall.RegQueryValueEx(k, n, nil, &typ, &buf[0], &size); e != nil {
		return 0, nil, e
	}
	return typ, buf[:size], nil
}

func regString(path, name string) (string, error) {
	typ, b, e := regValue(path, name)
	if e != nil {
		return "", e
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return "", fmt.Errorf("%s is not a string", name)
	}
	if len(b) < 2 {
		return "", nil
	}
	u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
	return strings.TrimSpace(syscall.UTF16ToString(u)), nil
}

func regDWORD(path, name string) (uint32, error) {
	typ, b, e := regValue(path, name)
	if e != nil {
		return 0, e
	}
	if typ != syscall.REG_DWORD || len(b) < 4 {
		return 0, fmt.Errorf("%s is not a DWORD", name)
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, nil
}
