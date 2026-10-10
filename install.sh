#!/bin/sh
# aibase installer for macOS and Linux.
#   curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh
# Env: AIBASE_VERSION, AIBASE_BIN_DIR, AIBASE_NO_SETUP=1, AIBASE_FROM_SOURCE=1,
#      AIBASE_TRAY=1 (desktop only), AIBASE_DRY_RUN=1 (no writes/downloads/setup)
set -eu

REPO="xiaoxin031/aibase"
say() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*) die "on Windows run: irm https://raw.githubusercontent.com/$REPO/main/install.ps1 | iex" ;;
  *) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ -n "${AIBASE_BIN_DIR:-}" ]; then
  bin_dir=$AIBASE_BIN_DIR
elif [ "$(id -u)" = 0 ]; then
  bin_dir=/usr/local/bin
else
  bin_dir=$HOME/.local/bin
fi
if [ "${AIBASE_DRY_RUN:-}" = 1 ]; then
  say "dry-run: install aibase-$os-$arch to $bin_dir; source=${AIBASE_FROM_SOURCE:-auto}"
  [ "${AIBASE_TRAY:-}" != 1 ] || say "dry-run: also install aibase-tray-$os-$arch (macOS app bundle on Darwin)"
  exit 0
fi
mkdir -p "$bin_dir"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else die "need curl or wget"; fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

local_src=""
case "$0" in
  */*|install.sh)
    candidate=$(dirname "$0")
    if [ -f "$0" ] && [ -f "$candidate/cmd/aibase/main.go" ] && [ -f "$candidate/go.mod" ] && grep -q '^module github.com/xiaoxin031/aibase$' "$candidate/go.mod"; then
      local_src=$candidate
    fi
    ;;
esac

build_from_source() {
  command -v go >/dev/null 2>&1 || die "no release download and Go is not installed (https://go.dev/dl)"
  src=$local_src
  if [ -z "$src" ]; then
    command -v git >/dev/null 2>&1 || die "need git to build from source"
    git clone --depth 1 "https://github.com/$REPO.git" "$tmp/src" >/dev/null 2>&1
    src=$tmp/src
  fi
  say "building from source"
  (cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$tmp/aibase" ./cmd/aibase)
  if [ "${AIBASE_TRAY:-}" = 1 ]; then
    tray_cgo=0
    [ "$os" != darwin ] || tray_cgo=1
    (cd "$src/tray" && go mod download && CGO_ENABLED=$tray_cgo go build -trimpath -ldflags "-s -w" -o "$tmp/aibase-tray" .)
  fi
}

verify() {
  want=$(awk -v asset="$1" '$2 == asset { print $1 }' "$tmp/checksums.txt")
  [ "${#want}" = 64 ] || die "missing or duplicate checksum for $1"
  [ "$(sha256 "$2")" = "$want" ] || die "checksum mismatch for $1"
}

asset="aibase-$os-$arch"
# A checkout builds locally when Go is available; otherwise it installs the release.
if [ "${AIBASE_FROM_SOURCE:-}" = 1 ] || { [ -n "$local_src" ] && command -v go >/dev/null 2>&1; }; then
  build_from_source
else
  version=${AIBASE_VERSION:-latest}
  if [ "$version" = latest ]; then base="https://github.com/$REPO/releases/latest/download"
  else base="https://github.com/$REPO/releases/download/$version"; fi
  say "downloading $asset ($version)"
  if fetch "$base/$asset" "$tmp/aibase" && fetch "$base/checksums.txt" "$tmp/checksums.txt"; then
    verify "$asset" "$tmp/aibase"
    if [ "${AIBASE_TRAY:-}" = 1 ]; then
      tray_asset="aibase-tray-$os-$arch"
      fetch "$base/$tray_asset" "$tmp/aibase-tray" || die "cannot download $tray_asset; use AIBASE_FROM_SOURCE=1 with a local checkout"
      verify "$tray_asset" "$tmp/aibase-tray"
    fi
  else
    build_from_source
  fi
fi

chmod 0755 "$tmp/aibase"
mv -f "$tmp/aibase" "$bin_dir/aibase"
say "installed $bin_dir/aibase ($("$bin_dir/aibase" version))"

if [ "${AIBASE_TRAY:-}" = 1 ]; then
  chmod 0755 "$tmp/aibase-tray"
  mv -f "$tmp/aibase-tray" "$bin_dir/aibase-tray"
  if [ "$os" = darwin ]; then
    app="$bin_dir/Aibase.app/Contents"
    mkdir -p "$app/MacOS"
    # Both executables stay adjacent even when Finder starts with a minimal PATH.
    ln -sf ../../../aibase "$app/MacOS/aibase"
    ln -sf ../../../aibase-tray "$app/MacOS/aibase-tray"
    cat >"$app/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.github.xiaoxin031.aibase</string>
<key>CFBundleName</key><string>Aibase</string>
<key>CFBundleExecutable</key><string>aibase-tray</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>NSHighResolutionCapable</key><true/>
<key>LSUIElement</key><true/>
</dict></plist>
PLIST
    say "desktop tray installed; launch with: open \"$bin_dir/Aibase.app\""
  else
    say "desktop tray installed; launch aibase-tray in a StatusNotifier desktop session"
  fi
fi

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *)
    line="export PATH=\"$bin_dir:\$PATH\""
    for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
      if [ -f "$rc" ] && ! grep -qs "$bin_dir" "$rc"; then
        printf '\n%s\n' "$line" >>"$rc"
        say "added $bin_dir to PATH in $rc"
      fi
    done
    [ -f "$HOME/.profile" ] || printf '%s\n' "$line" >"$HOME/.profile"
    PATH="$bin_dir:$PATH"
    ;;
esac

if [ "${AIBASE_NO_SETUP:-}" != 1 ]; then
  "$bin_dir/aibase" setup
fi
say "done. Open a new AI session (Claude Code, Codex, Gemini, Cursor, opencode) and it already knows this machine."
