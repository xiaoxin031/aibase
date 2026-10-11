#!/bin/sh
# aibase installer for macOS and Linux.
#   curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh
# Env: AIBASE_VERSION, AIBASE_BIN_DIR, AIBASE_NO_SETUP=1, AIBASE_FROM_SOURCE=1,
#      AIBASE_TRAY=0|1 (macOS menu bar app: on by default; Linux tray: opt-in),
#      AIBASE_DRY_RUN=1 (no writes/downloads/setup)
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
# The macOS menu bar app installs by default for desktop users, not root or SSH.
tray=${AIBASE_TRAY:-}
if [ -z "$tray" ] && [ "$os" = darwin ] && [ "$(id -u)" != 0 ] && [ -z "${SSH_CONNECTION:-}" ]; then
  tray=1
fi
app_dir="$HOME/Applications"
if [ "${AIBASE_DRY_RUN:-}" = 1 ]; then
  say "dry-run: install aibase-$os-$arch to $bin_dir; source=${AIBASE_FROM_SOURCE:-auto}"
  if [ "$tray" = 1 ] && [ "$os" = darwin ]; then say "dry-run: also install the menu bar app to $app_dir/Aibase.app"
  elif [ "$tray" = 1 ]; then say "dry-run: also install aibase-tray-$os-$arch"; fi
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
  ver=$(git -C "$src" describe --tags --always --dirty 2>/dev/null | sed 's/^v//') || ver=""
  (cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/xiaoxin031/aibase/internal/memory.version=${ver:-dev}" -o "$tmp/aibase" ./cmd/aibase)
  if [ "$tray" = 1 ] && [ "$os" = darwin ]; then
    if command -v swiftc >/dev/null 2>&1; then
      AIBASE_VERSION=${ver:-dev} "$src/macos/build.sh" "$tmp/app" >/dev/null || { say "menu bar app build failed; skipped"; tray=0; }
    else
      say "menu bar app skipped: install Xcode Command Line Tools (xcode-select --install) to build it"
      tray=0
    fi
  elif [ "$tray" = 1 ]; then
    (cd "$src/tray" && go mod download && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$tmp/aibase-tray" .)
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
    if [ "$tray" = 1 ] && [ "$os" = darwin ]; then
      if fetch "$base/Aibase-macos.zip" "$tmp/Aibase-macos.zip"; then
        verify Aibase-macos.zip "$tmp/Aibase-macos.zip"
        ditto -x -k "$tmp/Aibase-macos.zip" "$tmp/app"
      else
        say "menu bar app not in this release; skipped"
        tray=0
      fi
    elif [ "$tray" = 1 ]; then
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

if [ "$tray" = 1 ] && [ "$os" != darwin ]; then
  chmod 0755 "$tmp/aibase-tray"
  mv -f "$tmp/aibase-tray" "$bin_dir/aibase-tray"
  say "desktop tray installed; launch aibase-tray in a StatusNotifier desktop session"
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

if [ "$tray" = 1 ] && [ "$os" = darwin ]; then
  # Replace the old Go tray bundle; the Swift app supersedes it on macOS.
  if [ -e "$bin_dir/Aibase.app/Contents/MacOS/aibase-tray" ]; then
    rm -rf "$bin_dir/Aibase.app" "$bin_dir/aibase-tray"
  fi
  pkill -x Aibase 2>/dev/null || true
  mkdir -p "$app_dir"
  rm -rf "$app_dir/Aibase.app"
  mv "$tmp/app/Aibase.app" "$app_dir/Aibase.app"
  defaults write io.github.xiaoxin031.aibase coreBinary "$bin_dir/aibase" 2>/dev/null || true
  open "$app_dir/Aibase.app" 2>/dev/null || true
  say "menu bar app installed: $app_dir/Aibase.app (starts at login; AIBASE_TRAY=0 skips it)"
fi
say "done. Open a new AI session (Claude Code, Codex, Gemini, Cursor, opencode) and it already knows this machine."
