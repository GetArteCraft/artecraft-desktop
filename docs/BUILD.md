# Building ArteCraft from source

## Requirements

- Go 1.22 or newer (`go version`)
- Optional: a platform toolchain for the native window
  - Windows: MinGW-w64 when cross-compiling from Linux (`x86_64-w64-mingw32-gcc`)
  - Linux: `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`
  - macOS: Xcode Command Line Tools
- Optional: `zip`, for packaging release archives

## Build

```bash
cd src
CGO_ENABLED=0 go build -trimpath -o ArteCraft .          # any platform, local server mode
CGO_ENABLED=1 go build -trimpath -o ArteCraft .          # Linux, GTK + WebKitGTK window
```

Windows, from Windows or cross-compiling:

```bash
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -trimpath -ldflags "-s -w -H windowsgui" -o ArteCraft.exe .
```

macOS:

```bash
CGO_ENABLED=1 GOOS=darwin go build -trimpath -o ArteCraft .
```

Without CGO the binary still works: it serves the UI on `127.0.0.1` and opens the default
Chromium-based browser in app mode. With CGO you get the native window (WebView2 / WebKitGTK /
WKWebView).

## Checks before shipping

```bash
go vet ./...
go test ./...                # guards: allowed download hosts, i18n keys, no Cyrillic in Go strings and HTML
./ArteCraft --self-test      # starts the server, checks UI and assets, prints version and self SHA-256
```

## Versioning

The version lives in one place: `src/main.go`, `const LauncherVersion`. Change it there, update
the badge in `README.md`, add a `CHANGELOG.md` entry and build the release archives.

## Release archives

Suggested file names and contents:

| File | Contents |
|---|---|
| `ArteCraft-<ver>-Windows-Portable.zip` | `ArteCraft.exe`, icon, readme |
| `ArteCraft-Linux-x86_64.tar.gz` | `ArteCraft`, icon, `install-desktop-shortcut.sh`, readme |
| `ArteCraft-Linux-x86_64.AppImage` | AppDir with `usr/bin/ArteCraft`, desktop entry, icon |
| `ArteCraft-<ver>-ALL-IN-ONE.zip` | launchers, sources, screenshots, build guide, `CHECKSUMS.txt` |

Always publish a `SHA256SUMS.txt` next to the archives; the launcher and the website both show it.
