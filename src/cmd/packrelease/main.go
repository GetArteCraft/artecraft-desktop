package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	fatMagic   uint32 = 0xCAFEBABE
	pageAlign  uint32 = 14 // 2^14 = 16384 bytes (required for Apple Silicon arm64 slices)
	pageSize   int    = 1 << pageAlign
	lcCodeSign        = macho.LoadCmd(0x1d)
)

func must(err error, step string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %s: %v\n", step, err)
		os.Exit(1)
	}
}

func readLauncherVersion(launcherDir string) string {
	raw, err := os.ReadFile(filepath.Join(launcherDir, "main.go"))
	if err != nil {
		return "2.2.2"
	}
	re := regexp.MustCompile(`LauncherVersion\s*=\s*"([^"]+)"`)
	if m := re.FindSubmatch(raw); len(m) == 2 {
		return string(m[1])
	}
	return "2.2.2"
}

func goBuild(launcherDir, goos, goarch, outPath string) {
	fmt.Printf("  -> Compiling GOOS=%s GOARCH=%s ...\n", goos, goarch)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", outPath, ".")
	cmd.Dir = launcherDir
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOOS=") || strings.HasPrefix(kv, "GOARCH=") || strings.HasPrefix(kv, "CGO_ENABLED=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	must(cmd.Run(), fmt.Sprintf("go build %s/%s", goos, goarch))
}

func alignUp(n, align int) int {
	rem := n % align
	if rem == 0 {
		return n
	}
	return n + (align - rem)
}

// Combines darwin/amd64 and darwin/arm64 Mach-O binaries into a true Apple Universal Fat Mach-O (0xCAFEBABE)
// with 16 KB slice alignment, and verifies load commands + LC_CODE_SIGNATURE via debug/macho.
func createUniversalMachO(amd64Path, arm64Path string) []byte {
	bAmd, err := os.ReadFile(amd64Path)
	must(err, "read darwin/amd64 binary")
	bArm, err := os.ReadFile(arm64Path)
	must(err, "read darwin/arm64 binary")

	mAmd, err := macho.NewFile(bytes.NewReader(bAmd))
	must(err, "parse darwin/amd64 Mach-O")
	mArm, err := macho.NewFile(bytes.NewReader(bArm))
	must(err, "parse darwin/arm64 Mach-O")

	hasArmSig := false
	for _, ld := range mArm.Loads {
		raw := ld.Raw()
		if len(raw) >= 4 && macho.LoadCmd(binary.LittleEndian.Uint32(raw[:4])) == lcCodeSign {
			hasArmSig = true
			break
		}
	}
	if !hasArmSig {
		must(fmt.Errorf("darwin/arm64 binary missing LC_CODE_SIGNATURE"), "verify Apple Silicon ad-hoc signature")
	}

	offAmd := pageSize
	offArm := alignUp(offAmd+len(bAmd), pageSize)
	totalLen := offArm + len(bArm)

	out := make([]byte, totalLen)
	// fat_header (8 bytes, Big Endian)
	binary.BigEndian.PutUint32(out[0:4], fatMagic)
	binary.BigEndian.PutUint32(out[4:8], 2)

	// fat_arch #0: x86_64 (20 bytes, Big Endian)
	binary.BigEndian.PutUint32(out[8:12], uint32(mAmd.Cpu))
	binary.BigEndian.PutUint32(out[12:16], mAmd.SubCpu)
	binary.BigEndian.PutUint32(out[16:20], uint32(offAmd))
	binary.BigEndian.PutUint32(out[20:24], uint32(len(bAmd)))
	binary.BigEndian.PutUint32(out[24:28], pageAlign)

	// fat_arch #1: arm64 (20 bytes, Big Endian)
	binary.BigEndian.PutUint32(out[28:32], uint32(mArm.Cpu))
	binary.BigEndian.PutUint32(out[32:36], mArm.SubCpu)
	binary.BigEndian.PutUint32(out[36:40], uint32(offArm))
	binary.BigEndian.PutUint32(out[40:44], uint32(len(bArm)))
	binary.BigEndian.PutUint32(out[44:48], pageAlign)

	copy(out[offAmd:], bAmd)
	copy(out[offArm:], bArm)

	// Self-verify merged Universal binary using Go stdlib debug/macho.NewFatFile
	fatParsed, err := macho.NewFatFile(bytes.NewReader(out))
	must(err, "verify Universal Fat Mach-O via debug/macho")
	if len(fatParsed.Arches) != 2 {
		must(fmt.Errorf("expected 2 fat arches, got %d", len(fatParsed.Arches)), "verify fat arches")
	}
	_ = fatParsed.Close()
	fmt.Printf("  -> Verified Universal Mach-O (x86_64 + arm64, LC_CODE_SIGNATURE OK, %.1f MB)\n", float64(len(out))/(1024*1024))
	return out
}

// Wraps 512x512 PNG icon bytes in a valid Apple ICNS ('icns' + 'ic09') container
func buildAppleICNS(pngBytes []byte) []byte {
	chunkLen := uint32(8 + len(pngBytes))
	totalLen := uint32(8 + chunkLen)
	buf := make([]byte, totalLen)
	copy(buf[0:4], "icns")
	binary.BigEndian.PutUint32(buf[4:8], totalLen)
	copy(buf[8:12], "ic09") // 512x512 PNG icon type (macOS 10.5+)
	binary.BigEndian.PutUint32(buf[12:16], chunkLen)
	copy(buf[16:], pngBytes)
	return buf
}

type zipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

// Writes an Info-ZIP archive with explicit Unix CreatorVersion (3 << 8) so macOS Archive Utility
// preserves +x (0755) permissions on ArteCraft.app/Contents/MacOS/ArteCraft & .command scripts!
func writeUnixZip(destZip string, entries []zipEntry) {
	f, err := os.Create(destZip)
	must(err, "create "+destZip)
	defer f.Close()

	zw := zip.NewWriter(f)
	now := time.Now().UTC()
	for _, e := range entries {
		fh := &zip.FileHeader{
			Name:     e.name,
			Method:   zip.Deflate,
			Modified: now,
		}
		fh.SetMode(e.mode)
		fh.CreatorVersion = (3 << 8) | 20 // 3 = Unix
		fh.ReaderVersion = 20
		w, err := zw.CreateHeader(fh)
		must(err, "zip header "+e.name)
		_, err = w.Write(e.data)
		must(err, "zip write "+e.name)
	}
	must(zw.Close(), "finalize zip "+destZip)
}

type tarEntry struct {
	name string
	data []byte
	mode int64
}

func writeUnixTarGz(destTarGz string, entries []tarEntry) {
	f, err := os.Create(destTarGz)
	must(err, "create "+destTarGz)
	defer f.Close()

	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	must(err, "gzip writer")
	tw := tar.NewWriter(gz)
	now := time.Now().UTC()

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Size:     int64(len(e.data)),
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		must(tw.WriteHeader(hdr), "tar header "+e.name)
		_, err := tw.Write(e.data)
		must(err, "tar write "+e.name)
	}
	must(tw.Close(), "close tar")
	must(gz.Close(), "close gzip")
}

func fileSHA256(p string) (string, int64) {
	f, err := os.Open(p)
	must(err, "open for sha256 "+p)
	defer f.Close()
	st, err := f.Stat()
	must(err, "stat "+p)
	h := sha256.New()
	_, err = io.Copy(h, f)
	must(err, "hash "+p)
	return hex.EncodeToString(h.Sum(nil)), st.Size()
}

func main() {
	wd, err := os.Getwd()
	must(err, "getwd")

	// Support running from either repo root or ./launcher
	launcherDir := wd
	repoRoot := filepath.Dir(wd)
	if _, serr := os.Stat(filepath.Join(wd, "launcher", "main.go")); serr == nil {
		repoRoot = wd
		launcherDir = filepath.Join(wd, "launcher")
	}

	version := readLauncherVersion(launcherDir)
	outDir := filepath.Join(repoRoot, "release-upload")
	_ = os.RemoveAll(outDir)
	must(os.MkdirAll(outDir, 0755), "mkdir release-upload")

	tmpDir, err := os.MkdirTemp("", "artecraft-pack-*")
	must(err, "mktemp")
	defer os.RemoveAll(tmpDir)

	fmt.Printf("==============================================================\n")
	fmt.Printf("  ArteCraft v%s Cross-Platform Release Builder (macOS + Linux)\n", version)
	fmt.Printf("==============================================================\n\n")

	fmt.Println("[1/4] Running Go unit tests...")
	testCmd := exec.Command("go", "test", "./...")
	testCmd.Dir = launcherDir
	testCmd.Stdout = os.Stdout
	testCmd.Stderr = os.Stderr
	must(testCmd.Run(), "go test ./...")

	// Load 512x512 PNG icon
	iconPath := filepath.Join(repoRoot, "assets", "icon-512.png")
	iconBytes, err := os.ReadFile(iconPath)
	if err != nil {
		iconBytes, err = os.ReadFile(filepath.Join(launcherDir, "ui", "logo.png"))
		must(err, "load icon PNG")
	}

	fmt.Println("\n[2/4] Building macOS Universal App (Apple Silicon M1-M4 arm64 + Intel x86_64)...")
	darAmd := filepath.Join(tmpDir, "artecraft-darwin-amd64")
	darArm := filepath.Join(tmpDir, "artecraft-darwin-arm64")
	goBuild(launcherDir, "darwin", "amd64", darAmd)
	goBuild(launcherDir, "darwin", "arm64", darArm)
	universalBin := createUniversalMachO(darAmd, darArm)
	icnsBytes := buildAppleICNS(iconBytes)

	infoPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
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
  <string>%s</string>
  <key>CFBundleVersion</key>
  <string>%s</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
</dict>
</plist>
`, version, version)

	macFirstRun := strings.Join([]string{
		"#!/bin/bash",
		"set -e",
		`DIR="$(cd "$(dirname "$0")" && pwd)"`,
		`DEST="/Applications/ArteCraft.app"`,
		`echo "===================================================="`,
		`echo "  ArteCraft macOS Installer & First-Run Helper"`,
		`echo "===================================================="`,
		`if [ -d "$DIR/ArteCraft.app" ]; then`,
		`  echo "-> Installing ArteCraft.app to Applications..."`,
		`  rm -rf "$DEST" 2>/dev/null || true`,
		`  ditto "$DIR/ArteCraft.app" "$DEST" 2>/dev/null || {`,
		`    DEST="$HOME/Applications/ArteCraft.app"`,
		`    mkdir -p "$HOME/Applications"`,
		`    rm -rf "$DEST" 2>/dev/null || true`,
		`    ditto "$DIR/ArteCraft.app" "$DEST"`,
		`  }`,
		`fi`,
		`if [ ! -d "$DEST" ] && [ -d "$HOME/Applications/ArteCraft.app" ]; then`,
		`  DEST="$HOME/Applications/ArteCraft.app"`,
		`fi`,
		`if [ -d "$DEST" ]; then`,
		`  xattr -cr "$DEST" 2>/dev/null || true`,
		`  chmod +x "$DEST/Contents/MacOS/ArteCraft" 2>/dev/null || true`,
		`  echo "-> Launching ArteCraft..."`,
		`  open "$DEST"`,
		`  exit 0`,
		`elif [ -d "$DIR/ArteCraft.app" ]; then`,
		`  xattr -cr "$DIR/ArteCraft.app" 2>/dev/null || true`,
		`  chmod +x "$DIR/ArteCraft.app/Contents/MacOS/ArteCraft" 2>/dev/null || true`,
		`  open "$DIR/ArteCraft.app"`,
		`  exit 0`,
		`fi`,
		"",
	}, "\n")

	macZipPath := filepath.Join(outDir, "ArteCraft-macOS-Universal.zip")
	writeUnixZip(macZipPath, []zipEntry{
		{name: "ArteCraft.app/Contents/Info.plist", data: []byte(strings.ReplaceAll(infoPlist, "\r\n", "\n")), mode: 0644},
		{name: "ArteCraft.app/Contents/MacOS/ArteCraft", data: universalBin, mode: 0755},
		{name: "ArteCraft.app/Contents/Resources/AppIcon.icns", data: icnsBytes, mode: 0644},
		{name: "Open ArteCraft (First Run).command", data: []byte(macFirstRun), mode: 0755},
	})

	fmt.Println("\n[3/4] Building Linux x86_64 (.AppImage & .tar.gz) + ARM64 (.tar.gz)...")
	linX64 := filepath.Join(tmpDir, "artecraft-linux-amd64")
	linArm := filepath.Join(tmpDir, "artecraft-linux-arm64")
	goBuild(launcherDir, "linux", "amd64", linX64)
	goBuild(launcherDir, "linux", "arm64", linArm)

	linX64Bytes, err := os.ReadFile(linX64)
	must(err, "read linux/amd64")
	linArmBytes, err := os.ReadFile(linArm)
	must(err, "read linux/arm64")

	appImageOut := filepath.Join(outDir, "ArteCraft-Linux-x86_64.AppImage")
	must(os.WriteFile(appImageOut, linX64Bytes, 0755), "write AppImage")

	linuxInstaller := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -e",
		`DIR="$(cd "$(dirname "$0")" && pwd)"`,
		`DEST_DIR="$HOME/.local/opt/artecraft-launcher"`,
		`mkdir -p "$DEST_DIR" "$HOME/.local/bin" "$HOME/.local/share/applications"`,
		`cp -f "$DIR/ArteCraft" "$DEST_DIR/ArteCraft"`,
		`chmod 0755 "$DEST_DIR/ArteCraft"`,
		`[ -f "$DIR/artecraft.png" ] && cp -f "$DIR/artecraft.png" "$DEST_DIR/artecraft.png"`,
		`ln -sf "$DEST_DIR/ArteCraft" "$HOME/.local/bin/artecraft"`,
		`DESKTOP_FILE="$HOME/.local/share/applications/artecraft.desktop"`,
		`cat > "$DESKTOP_FILE" <<EOF`,
		`[Desktop Entry]`,
		`Type=Application`,
		`Name=ArteCraft`,
		`Comment=13 Free Native Rust Creative, Office & CAD Apps`,
		`Exec="$DEST_DIR/ArteCraft"`,
		`Icon=$DEST_DIR/artecraft.png`,
		`Terminal=false`,
		`Categories=Graphics;Office;Utility;`,
		`StartupNotify=true`,
		`EOF`,
		`chmod 0755 "$DESKTOP_FILE"`,
		`for d in "$HOME/Desktop" "$HOME/Рабочий стол"; do`,
		`  if [ -d "$d" ]; then`,
		`    cp -f "$DESKTOP_FILE" "$d/ArteCraft.desktop"`,
		`    chmod 0755 "$d/ArteCraft.desktop"`,
		`  fi`,
		`done`,
		`echo "[OK] ArteCraft installed! Launching..."`,
		`nohup "$DEST_DIR/ArteCraft" >/dev/null 2>&1 &`,
		"",
	}, "\n")

	linuxReadme := strings.Join([]string{
		"ArteCraft Desktop Hub for Linux",
		"===============================",
		"1) Quick portable start:",
		"   ./ArteCraft",
		"",
		"2) Install to Applications Menu & Desktop:",
		"   ./install-desktop-shortcut.sh",
		"",
	}, "\n")

	writeUnixTarGz(filepath.Join(outDir, "ArteCraft-Linux-x86_64.tar.gz"), []tarEntry{
		{name: "ArteCraft-Linux-x86_64/ArteCraft", data: linX64Bytes, mode: 0755},
		{name: "ArteCraft-Linux-x86_64/install-desktop-shortcut.sh", data: []byte(linuxInstaller), mode: 0755},
		{name: "ArteCraft-Linux-x86_64/artecraft.png", data: iconBytes, mode: 0644},
		{name: "ArteCraft-Linux-x86_64/README-LINUX.txt", data: []byte(linuxReadme), mode: 0644},
	})

	writeUnixTarGz(filepath.Join(outDir, "ArteCraft-Linux-aarch64.tar.gz"), []tarEntry{
		{name: "ArteCraft-Linux-aarch64/ArteCraft", data: linArmBytes, mode: 0755},
		{name: "ArteCraft-Linux-aarch64/install-desktop-shortcut.sh", data: []byte(linuxInstaller), mode: 0755},
		{name: "ArteCraft-Linux-aarch64/artecraft.png", data: iconBytes, mode: 0644},
		{name: "ArteCraft-Linux-aarch64/README-LINUX.txt", data: []byte(linuxReadme), mode: 0644},
	})

	fmt.Println("\n[4/4] Generating SHA256SUMS.txt...")
	files := []string{
		"ArteCraft-macOS-Universal.zip",
		"ArteCraft-Linux-x86_64.AppImage",
		"ArteCraft-Linux-x86_64.tar.gz",
		"ArteCraft-Linux-aarch64.tar.gz",
	}
	var sumLines []string
	fmt.Println("\nBuilt files in release-upload/:")
	for _, fn := range files {
		full := filepath.Join(outDir, fn)
		sum, sz := fileSHA256(full)
		sumLines = append(sumLines, fmt.Sprintf("%s  %s", sum, fn))
		fmt.Printf("  [OK] %-34s  %6.2f MB  sha256=%s...\n", fn, float64(sz)/(1024*1024), sum[:16])
	}
	must(os.WriteFile(filepath.Join(outDir, "SHA256SUMS.txt"), []byte(strings.Join(sumLines, "\n")+"\n"), 0644), "write SHA256SUMS.txt")
	fmt.Printf("\n[SUCCESS] All macOS & Linux release files are ready in:\n  %s\n", outDir)
}
