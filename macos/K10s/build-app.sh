#!/usr/bin/env bash
# Builds dist/K10s.app: SwiftUI app + universal k10s-engine bundled in
# Contents/Resources/bin. Requires macOS with Xcode command line tools and Go.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$(pwd)
VERSION=${VERSION:-0.1.0}
DIST="$ROOT/dist"
APP="$DIST/K10s.app"
rm -rf "$APP" && mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/bin"

echo "==> Go engine (universal)"
for bin in k10s-engine; do
  for arch in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o "$DIST/$bin-$arch" "./cmd/$bin"
  done
  lipo -create -output "$APP/Contents/Resources/bin/$bin" "$DIST/$bin-arm64" "$DIST/$bin-amd64"
  rm "$DIST/$bin-arm64" "$DIST/$bin-amd64"
done

echo "==> Swift app (universal)"
( cd macos/K10s && swift build -c release --product K10s --arch arm64 --arch x86_64 )
BIN=$(cd macos/K10s && swift build -c release --product K10s --arch arm64 --arch x86_64 --show-bin-path)
cp "$BIN/K10s" "$APP/Contents/MacOS/K10s"
sed "s/__VERSION__/$VERSION/g" macos/K10s/Info.plist > "$APP/Contents/Info.plist"

echo "==> Sparkle.framework"
FW_SRC="$BIN/Sparkle.framework"
if [ ! -d "$FW_SRC" ]; then
  FW_SRC=$(find macos/K10s/.build -type d -name Sparkle.framework -path "*macos*" | head -1)
fi
test -d "$FW_SRC" || { echo "Sparkle.framework not found in the Swift build"; exit 1; }
mkdir -p "$APP/Contents/Frameworks"
ditto "$FW_SRC" "$APP/Contents/Frameworks/Sparkle.framework"
FW="$APP/Contents/Frameworks/Sparkle.framework"
FWV="$FW/Versions/$(readlink "$FW/Versions/Current")"
# XPC services are only needed by sandboxed apps; K10s isn't sandboxed.
rm -rf "$FWV/XPCServices" "$FW/XPCServices"

# Auto-update is enabled only in builds that carry the update-signing key.
if [ -n "${SPARKLE_ED_PRIVATE_KEY:-}" ]; then
  PUB=$(go run ./cmd/k10s-release sparkle-pubkey)
  /usr/libexec/PlistBuddy -c "Add :SUPublicEDKey string $PUB" "$APP/Contents/Info.plist"
  echo "auto-update enabled (SUPublicEDKey $PUB)"
else
  echo "auto-update disabled: no SPARKLE_ED_PRIVATE_KEY"
fi
if [ -d macos/K10s/icon/AppIcon.iconset ]; then
  iconutil -c icns macos/K10s/icon/AppIcon.iconset -o "$APP/Contents/Resources/AppIcon.icns"
else
  echo "no macos/K10s/icon/AppIcon.iconset: building without an icon"
fi
mkdir -p "$APP/Contents/Resources/Fonts"
cp macos/K10s/Fonts/*.ttf macos/K10s/Fonts/OFL-*.txt "$APP/Contents/Resources/Fonts/"

echo "==> Sign"
IDENTITY=${CODESIGN_IDENTITY:--}   # "-" = ad-hoc; set to "Developer ID Application: ..." for distribution
OPTS=(--force --timestamp=none --sign "$IDENTITY")
if [ "$IDENTITY" != "-" ]; then OPTS=(--force --options runtime --timestamp --sign "$IDENTITY"); fi
# Inside out: Sparkle's helpers, the framework, our binaries, then the app.
codesign "${OPTS[@]}" "$FWV/Autoupdate"
codesign "${OPTS[@]}" "$FWV/Updater.app"
codesign "${OPTS[@]}" "$FW"
codesign "${OPTS[@]}" "$APP/Contents/Resources/bin/k10s-engine"
codesign "${OPTS[@]}" "$APP"
codesign --verify --deep --strict "$APP"

echo "==> Zip"
( cd "$DIST" && ditto -c -k --keepParent K10s.app "K10s-$VERSION.zip" )
echo "built $APP and $DIST/K10s-$VERSION.zip"
