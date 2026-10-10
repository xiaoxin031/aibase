# aibase installer for Windows.
# irm https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.ps1 | iex
# Env: AIBASE_VERSION, AIBASE_BIN_DIR, AIBASE_NO_SETUP=1, AIBASE_FROM_SOURCE=1,
#      AIBASE_TRAY=1 (desktop only), AIBASE_DRY_RUN=1
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'xiaoxin031/aibase'
function Say($m) { Write-Host "==> $m" -ForegroundColor Cyan }
$nativeArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($nativeArch) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $nativeArch" }
}
$asset = "aibase-windows-$arch.exe"
$trayAsset = "aibase-tray-windows-$arch.exe"
$binDir = if ($env:AIBASE_BIN_DIR) { $env:AIBASE_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\aibase' }
if ($env:AIBASE_DRY_RUN -eq '1') {
  Say "dry-run: install $asset to $binDir; source=$env:AIBASE_FROM_SOURCE"
  if ($env:AIBASE_TRAY -eq '1') { Say "dry-run: also install $trayAsset" }
  return
}
$version = if ($env:AIBASE_VERSION) { $env:AIBASE_VERSION } else { 'latest' }
$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("aibase-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
function Verify-Asset($name, $file, $sums) {
  $matchesForAsset = @($sums | Where-Object { $_ -match "^[a-fA-F0-9]{64}\s+$([regex]::Escape($name))\s*$" })
  if ($matchesForAsset.Count -ne 1) { throw "missing or duplicate checksum for $name" }
  $want = ($matchesForAsset[0] -split '\s+')[0]
  if ((Get-FileHash -Algorithm SHA256 $file).Hash -ne $want) { throw "checksum mismatch for $name" }
}
function Build-Source {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go is required to build from source (https://go.dev/dl)' }
  $src = $PSScriptRoot
  if (-not $src -or -not (Test-Path (Join-Path $src 'go.mod'))) {
    $src = Join-Path $tmp 'src'
    & git clone --depth 1 "https://github.com/$repo.git" $src
    if ($LASTEXITCODE -ne 0) { throw 'source clone failed' }
  }
  $oldCGO = $env:CGO_ENABLED
  try {
    $env:CGO_ENABLED = '0'
    Push-Location $src
    try {
      & go build -trimpath -ldflags '-s -w' -o (Join-Path $tmp 'aibase.exe') ./cmd/aibase
      if ($LASTEXITCODE -ne 0) { throw 'core build failed' }
    } finally { Pop-Location }
    if ($env:AIBASE_TRAY -eq '1') {
      Push-Location (Join-Path $src 'tray')
      try {
        & go mod download
        if ($LASTEXITCODE -ne 0) { throw 'tray dependency download failed' }
        & go build -trimpath -ldflags '-s -w -H=windowsgui' -o (Join-Path $tmp 'aibase-tray.exe') .
        if ($LASTEXITCODE -ne 0) { throw 'tray build failed' }
      } finally { Pop-Location }
    }
  } finally { $env:CGO_ENABLED = $oldCGO }
}
try {
  $exe = Join-Path $tmp 'aibase.exe'
  $trayExe = Join-Path $tmp 'aibase-tray.exe'
  $hasGo = [bool](Get-Command go -ErrorAction SilentlyContinue)
  $source = $env:AIBASE_FROM_SOURCE -eq '1' -or ($hasGo -and $PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot 'go.mod')))
  if (-not $source) {
    Say "downloading $asset ($version)"
    try {
      Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile $exe
      $sumsFile = Join-Path $tmp 'checksums.txt'
      Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile $sumsFile
      $sums = Get-Content -LiteralPath $sumsFile
    } catch {
      Say 'release download unavailable; building from source'
      $source = $true
    }
    if (-not $source) {
      Verify-Asset $asset $exe $sums
      if ($env:AIBASE_TRAY -eq '1') {
        Invoke-WebRequest -UseBasicParsing "$base/$trayAsset" -OutFile $trayExe
        Verify-Asset $trayAsset $trayExe $sums
      }
    }
  }
  if ($source) { Build-Source }
  New-Item -ItemType Directory -Force -Path $binDir | Out-Null
  $dest = Join-Path $binDir 'aibase.exe'
  Move-Item -Force $exe $dest
  if ($env:AIBASE_TRAY -eq '1') {
    Move-Item -Force $trayExe (Join-Path $binDir 'aibase-tray.exe')
    Say 'desktop tray installed; launch aibase-tray.exe (not started automatically)'
  }
  Say "installed $dest ($(& $dest version))"
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $binDir)) {
  [Environment]::SetEnvironmentVariable('Path', (($userPath, $binDir) -ne '' -join ';'), 'User')
  Say "added $binDir to your user PATH (new terminals pick it up)"
}
if (-not (($env:Path -split ';') -contains $binDir)) { $env:Path = "$binDir;$env:Path" }
if ($env:AIBASE_NO_SETUP -ne '1') { & (Join-Path $binDir 'aibase.exe') setup }
Say 'done. Open a new AI session (Claude Code, Codex, Gemini, Cursor, opencode) and it already knows this machine.'
