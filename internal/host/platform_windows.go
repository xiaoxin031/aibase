//go:build windows

package host

import "time"

func (i *Inspector) ps(name string, ttl time.Duration, script string) probe {
	return i.command(name, ttl, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); $OutputEncoding = [Console]::OutputEncoding; `+script)
}
func (i *Inspector) platform() []probe {
	d := 24 * time.Hour
	return []probe{
		i.ps("os_version", d, `Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' | Select-Object ProductName,DisplayVersion,CurrentBuild | ConvertTo-Json -Compress`),
		i.ps("kernel", d, `[Environment]::OSVersion.Version.ToString()`), i.ps("hostname", 0, `[Environment]::MachineName`),
		i.ps("admin", 0, `([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)`),
		i.ps("cpu_model", d, `Get-ItemProperty 'HKLM:\HARDWARE\DESCRIPTION\System\CentralProcessor\0' | Select-Object ProcessorNameString | ConvertTo-Json -Compress`),
		i.ps("memory_total", d, `(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory`),
		i.ps("disk", time.Minute, `Get-CimInstance Win32_LogicalDisk -Filter ("DeviceID='" + $env:SystemDrive + "'") | Select-Object DeviceID,FreeSpace,Size | ConvertTo-Json -Compress`),
		i.ps("gpu", 7*d, `Get-CimInstance Win32_VideoController | Select-Object Name,AdapterRAM | ConvertTo-Json -Compress`),
		i.ps("vm", d, `Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer,Model,HypervisorPresent | ConvertTo-Json -Compress`),
		i.ps("language", 0, `(Get-Culture).Name`),
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
