#!/usr/bin/env bash
# Build a drag-to-Applications .dmg for Chatwoot MCP on macOS.
#
# Usage: build-dmg.sh <version> <arch> <binary> <out-dir>
# Requires macOS tools: sips, iconutil, codesign, hdiutil.
set -euo pipefail

if [ "$#" -ne 4 ]; then
	echo "usage: $0 <version> <arch> <binary> <out-dir>" >&2
	exit 2
fi

VERSION="$1"
ARCH="$2"
BINARY="$3"
OUT="$4"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
APP_NAME="Chatwoot MCP"
STAGE="$OUT/dmg"
APP="$STAGE/$APP_NAME.app"

rm -rf "$STAGE"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

install -m 0755 "$BINARY" "$APP/Contents/MacOS/chatwoot-mcp"

ICONSET="$OUT/AppIcon.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for size in 16 32 64 128 256 512; do
	sips -z "$size" "$size" "$ROOT/packaging/AppIcon.png" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
done
# Retina variants expected by iconutil.
cp "$ICONSET/icon_32x32.png" "$ICONSET/icon_16x16@2x.png"
cp "$ICONSET/icon_64x64.png" "$ICONSET/icon_32x32@2x.png"
cp "$ICONSET/icon_256x256.png" "$ICONSET/icon_128x128@2x.png"
cp "$ICONSET/icon_512x512.png" "$ICONSET/icon_256x256@2x.png"
cp "$ICONSET/icon_512x512.png" "$ICONSET/icon_512x512@2x.png"
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
rm -rf "$ICONSET"

sed "s/__VERSION__/$VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$APP/Contents/Info.plist"

# Ad-hoc signature so the app launches locally without a developer identity.
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "warning: ad-hoc codesign failed" >&2

ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "$APP_NAME" -srcfolder "$STAGE" -ov -format UDZO \
	"$OUT/chatwoot-mcp_${VERSION}_darwin_${ARCH}.dmg" >/dev/null
rm -rf "$STAGE"

echo "created $OUT/chatwoot-mcp_${VERSION}_darwin_${ARCH}.dmg"
