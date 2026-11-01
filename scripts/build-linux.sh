#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-2.2.2}"
VERSION="${VERSION#v}"
DIST_DIR="$ROOT_DIR/dist"

echo "=== Building ArteCraft v${VERSION} Linux (x86_64 AppImage + x86_64 tar.gz + aarch64 tar.gz) ==="
mkdir -p "$DIST_DIR"
cd "$ROOT_DIR/launcher"

echo "[1/4] Running automated Go test suite..."
CGO_ENABLED=1 go test -v ./...

echo "[2/4] Building Linux x86_64 (dlopen GTK3/WebKit2GTK + browser fallback, zero libfuse2 needed)..."
STAGE_X64="$(mktemp -d)"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$STAGE_X64/ArteCraft" .
chmod 0755 "$STAGE_X64/ArteCraft"
"$STAGE_X64/ArteCraft" --self-test

# Portable single-file executable (works on Ubuntu 20.04/22.04/24.04+, Debian, Fedora, Mint, Arch without libfuse2!)
cp -f "$STAGE_X64/ArteCraft" "$DIST_DIR/ArteCraft-Linux-x86_64.AppImage"
chmod 0755 "$DIST_DIR/ArteCraft-Linux-x86_64.AppImage"

echo "[3/4] Packaging ArteCraft-Linux-x86_64.tar.gz..."
PKG_X64="$STAGE_X64/ArteCraft-Linux-x86_64"
mkdir -p "$PKG_X64"
cp -f "$STAGE_X64/ArteCraft" "$PKG_X64/ArteCraft"
chmod 0755 "$PKG_X64/ArteCraft"

if [ -f "$ROOT_DIR/assets/icon-512.png" ]; then
  cp -f "$ROOT_DIR/assets/icon-512.png" "$PKG_X64/artecraft.png"
elif [ -f "$ROOT_DIR/launcher/ui/logo.png" ]; then
  cp -f "$ROOT_DIR/launcher/ui/logo.png" "$PKG_X64/artecraft.png"
fi

cat > "$PKG_X64/install-desktop-shortcut.sh" << 'INSTALL_SH'
#!/usr/bin/env bash
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
DEST_DIR="$HOME/.local/opt/artecraft-launcher"
mkdir -p "$DEST_DIR" "$HOME/.local/bin" "$HOME/.local/share/applications"

cp -f "$DIR/ArteCraft" "$DEST_DIR/ArteCraft"
chmod 0755 "$DEST_DIR/ArteCraft"
if [ -f "$DIR/artecraft.png" ]; then
  cp -f "$DIR/artecraft.png" "$DEST_DIR/artecraft.png"
fi
ln -sf "$DEST_DIR/ArteCraft" "$HOME/.local/bin/artecraft"

DESKTOP_FILE="$HOME/.local/share/applications/artecraft.desktop"
cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Name=ArteCraft
Comment=13 Free Native Rust Creative, Office & CAD Apps
Exec="$DEST_DIR/ArteCraft"
Icon=$DEST_DIR/artecraft.png
Terminal=false
Categories=Graphics;Office;Utility;
StartupNotify=true
EOF
chmod 0755 "$DESKTOP_FILE"

for d in "$HOME/Desktop" "$HOME/Рабочий стол"; do
  if [ -d "$d" ]; then
    cp -f "$DESKTOP_FILE" "$d/ArteCraft.desktop"
    chmod 0755 "$d/ArteCraft.desktop"
  fi
done

echo "[OK] ArteCraft installed! Launching now..."
nohup "$DEST_DIR/ArteCraft" >/dev/null 2>&1 &
INSTALL_SH
chmod 0755 "$PKG_X64/install-desktop-shortcut.sh"

cat > "$PKG_X64/README-LINUX.txt" << 'README_EOF'
ArteCraft Desktop Hub for Linux (x86_64)
========================================
Quick Run (no installation needed):
  ./ArteCraft

Install to System Application Menu & Desktop:
  ./install-desktop-shortcut.sh

Optional native GTK WebView package (if you prefer native window over Chromium/Firefox app mode):
  Ubuntu 24.04 / Debian 13: sudo apt install libwebkit2gtk-4.1-0
  Ubuntu 22.04 / Debian 12: sudo apt install libwebkit2gtk-4.0-37
  Fedora:                   sudo dnf install webkit2gtk4.1
  Arch / Manjaro:           sudo pacman -S webkit2gtk-4.1
README_EOF

tar -C "$STAGE_X64" -czf "$DIST_DIR/ArteCraft-Linux-x86_64.tar.gz" "ArteCraft-Linux-x86_64"
rm -rf "$STAGE_X64"

echo "[4/4] Building ArteCraft-Linux-aarch64.tar.gz (ARM64 Linux)..."
STAGE_ARM="$(mktemp -d)"
PKG_ARM="$STAGE_ARM/ArteCraft-Linux-aarch64"
mkdir -p "$PKG_ARM"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$PKG_ARM/ArteCraft" .
chmod 0755 "$PKG_ARM/ArteCraft"
if [ -f "$ROOT_DIR/assets/icon-512.png" ]; then
  cp -f "$ROOT_DIR/assets/icon-512.png" "$PKG_ARM/artecraft.png"
fi
tar -C "$STAGE_ARM" -czf "$DIST_DIR/ArteCraft-Linux-aarch64.tar.gz" "ArteCraft-Linux-aarch64"
rm -rf "$STAGE_ARM"

ls -lh "$DIST_DIR"/ArteCraft-Linux-*
echo "=== Linux builds complete! ==="
