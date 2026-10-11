#!/bin/sh
# Builds the universal Aibase.app menu bar app with the system Swift toolchain
# (Xcode Command Line Tools are enough). Usage: macos/build.sh [out-dir]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
out=$(mkdir -p "${1:-$here/../dist}" && cd "${1:-$here/../dist}" && pwd)
version="${AIBASE_VERSION:-$(git -C "$here" describe --tags --always 2>/dev/null | sed 's/^v//')}"
version="${version:-dev}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for arch in arm64 x86_64; do
  swiftc -O -parse-as-library -swift-version 5 -target "$arch-apple-macos13" \
    "$here"/Sources/*.swift -o "$tmp/Aibase-$arch"
done
lipo -create "$tmp/Aibase-arm64" "$tmp/Aibase-x86_64" -output "$tmp/Aibase"

app="$out/Aibase.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
mv "$tmp/Aibase" "$app/Contents/MacOS/Aibase"
"$app/Contents/MacOS/Aibase" --render-iconset "$tmp/AppIcon.iconset"
iconutil -c icns "$tmp/AppIcon.iconset" -o "$app/Contents/Resources/AppIcon.icns"
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>aibase</string>
<key>CFBundleDisplayName</key><string>aibase</string>
<key>CFBundleIdentifier</key><string>io.github.xiaoxin031.aibase</string>
<key>CFBundleExecutable</key><string>Aibase</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>$version</string>
<key>CFBundleVersion</key><string>$version</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
codesign --force --sign - "$app" >/dev/null
echo "$app"
