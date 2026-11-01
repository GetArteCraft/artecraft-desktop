#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-2.2.2}"
VERSION="${VERSION#v}"
DIST_DIR="$ROOT_DIR/dist"
STAGE_DIR="$DIST_DIR/macos-stage"
DMG_ROOT="$DIST_DIR/dmg-root"
APP_BUNDLE="$DMG_ROOT/ArteCraft.app"

echo "=== Building ArteCraft v${VERSION} Universal macOS (.app + .dmg + .zip) ==="
rm -rf "$STAGE_DIR" "$DMG_ROOT"
mkdir -p "$STAGE_DIR" "$APP_BUNDLE/Contents/MacOS" "$APP_BUNDLE/Contents/Resources" "$DIST_DIR"

cd "$ROOT_DIR/launcher"

echo "[1/6] Running Go unit tests..."
go test -v ./...

echo "[2/6] Compiling native Cocoa/WKWebView arm64 (Apple Silicon M1/M2/M3/M4)..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
  CGO_CFLAGS="-mmacosx-version-min=11.0" \
  CGO_LDFLAGS="-mmacosx-version-min=11.0" \
  go build -trimpath -ldflags="-s -w" -o "$STAGE_DIR/ArteCraft-arm64" .

echo "[3/6] Compiling native Cocoa/WKWebView amd64 (Intel Mac)..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
  CGO_CFLAGS="-mmacosx-version-min=11.0" \
  CGO_LDFLAGS="-mmacosx-version-min=11.0" \
  go build -trimpath -ldflags="-s -w" -o "$STAGE_DIR/ArteCraft-amd64" .

echo "[4/6] Merging Universal Mach-O binary via lipo..."
lipo -create -output "$APP_BUNDLE/Contents/MacOS/ArteCraft" \
  "$STAGE_DIR/ArteCraft-arm64" \
  "$STAGE_DIR/ArteCraft-amd64"
chmod 0755 "$APP_BUNDLE/Contents/MacOS/ArteCraft"
lipo -info "$APP_BUNDLE/Contents/MacOS/ArteCraft"

echo "[5/6] Generating Retina AppIcon.icns & Info.plist..."
SRC_ICON="$ROOT_DIR/assets/icon-512.png"
if [ ! -f "$SRC_ICON" ]; then
  SRC_ICON="$ROOT_DIR/launcher/ui/logo.png"
fi
if [ -f "$SRC_ICON" ] && command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then
  ICONSET="$STAGE_DIR/AppIcon.iconset"
  mkdir -p "$ICONSET"
  sips -z 16 16     "$SRC_ICON" --out "$ICONSET/icon_16x16.png" >/dev/null
  sips -z 32 32     "$SRC_ICON" --out "$ICONSET/icon_16x16@2x.png" >/dev/null
  sips -z 32 32     "$SRC_ICON" --out "$ICONSET/icon_32x32.png" >/dev/null
  sips -z 64 64     "$SRC_ICON" --out "$ICONSET/icon_32x32@2x.png" >/dev/null
  sips -z 128 128   "$SRC_ICON" --out "$ICONSET/icon_128x128.png" >/dev/null
  sips -z 256 256   "$SRC_ICON" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
  sips -z 256 256   "$SRC_ICON" --out "$ICONSET/icon_256x256.png" >/dev/null
  sips -z 512 512   "$SRC_ICON" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
  sips -z 512 512   "$SRC_ICON" --out "$ICONSET/icon_512x512.png" >/dev/null
  cp "$SRC_ICON" "$ICONSET/icon_512x512@2x.png"
  iconutil -c icns "$ICONSET" -o "$APP_BUNDLE/Contents/Resources/AppIcon.icns"
fi

cat > "$APP_BUNDLE/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key>
  <string>ArteCraft</string>
  <key>CFBundleExecutable</key>
  <string>ArteCraft</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleIdentifier</key>
  <string>com.getartecraft.launcher</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>ArteCraft</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>NSSupportsAutomaticGraphicsSwitching</key>
  <true/>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
</dict>
</plist>
EOF

echo "Ad-hoc signing ArteCraft.app (required by Apple Silicon arm64 kernel)..."
xattr -cr "$APP_BUNDLE" || true
codesign --force --deep --sign - "$APP_BUNDLE"
codesign --verify --deep --strict --verbose=2 "$APP_BUNDLE"

echo "Running live --self-test on Universal binary..."
"$APP_BUNDLE/Contents/MacOS/ArteCraft" --self-test

# Add Applications drag-and-drop symlink + 1-click Gatekeeper First-Run helper
ln -s /Applications "$DMG_ROOT/Applications"

cat > "$DMG_ROOT/Open ArteCraft (First Run).command" << 'HELPER'
#!/bin/bash
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
DEST="/Applications/ArteCraft.app"

echo "===================================================="
echo "  ArteCraft macOS Installer & First-Run Helper"
echo "===================================================="

if [ -d "$DIR/ArteCraft.app" ] && [ ! -d "$DEST" ]; then
  echo "-> Copying ArteCraft.app to /Applications..."
  ditto "$DIR/ArteCraft.app" "$DEST" || {
    DEST="$HOME/Applications/ArteCraft.app"
    mkdir -p "$HOME/Applications"
    ditto "$DIR/ArteCraft.app" "$DEST"
  }
fi

if [ ! -d "$DEST" ] && [ -d "$HOME/Applications/ArteCraft.app" ]; then
  DEST="$HOME/Applications/ArteCraft.app"
fi

if [ -d "$DEST" ]; then
  echo "-> Clearing macOS browser quarantine flag..."
  xattr -cr "$DEST" 2>/dev/null || true
  echo "-> Launching ArteCraft..."
  open "$DEST"
  exit 0
elif [ -d "$DIR/ArteCraft.app" ]; then
  open "$DIR/ArteCraft.app"
  exit 0
fi
HELPER
chmod 0755 "$DMG_ROOT/Open ArteCraft (First Run).command"

echo "[6/6] Packaging ArteCraft-macOS-Universal.dmg & ArteCraft-macOS-Universal.zip..."
rm -f "$DIST_DIR/ArteCraft-macOS-Universal.dmg" "$DIST_DIR/ArteCraft-macOS-Universal.zip"
hdiutil create -volname "ArteCraft" -srcfolder "$DMG_ROOT" -ov -format UDZO "$DIST_DIR/ArteCraft-macOS-Universal.dmg"
ditto -c -k --sequesterRsrc --keepParent "$APP_BUNDLE" "$DIST_DIR/ArteCraft-macOS-Universal.zip"

rm -rf "$STAGE_DIR" "$DMG_ROOT"
ls -lh "$DIST_DIR"/ArteCraft-macOS-Universal.*
echo "=== macOS Universal build complete! ==="
