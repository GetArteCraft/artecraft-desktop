<div align="center">

# ArteCraft Desktop Hub

**One launcher instead of Adobe, Microsoft and Autodesk subscriptions.**
13 bundled open-source apps, plus a marketplace where any author can add their own software straight from GitHub.

[![Release](https://img.shields.io/badge/release-2.5.4-4e7bfb?style=flat-square)](https://getartecraft.com/download)
[![License](https://img.shields.io/badge/license-MIT-22d3ee?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-8b5cf6?style=flat-square)](#build-from-source)
[![No telemetry](https://img.shields.io/badge/telemetry-none-34d399?style=flat-square)](#privacy)

[Website](https://getartecraft.com) · [Download](https://getartecraft.com/download) · [Marketplace API](https://getartecraft.com/api/market/catalog) · [Build](#build-from-source) · [Contributing](CONTRIBUTING.md)

</div>

---

## What it is

ArteCraft is a desktop hub that replaces a stack of paid applications with open-source
alternatives: PhotoCraft instead of Photoshop, WordCraft instead of Word, GridCraft instead of
Excel, FigCraft instead of Figma and Visio, CADCraft instead of AutoCAD, FilmCraft instead of
Premiere, SoundCraft instead of Pro Tools, LightCraft instead of Lightroom, VectorCraft instead
of Illustrator, EffectCraft instead of After Effects, DesignCraft instead of InDesign,
DeckCraft instead of PowerPoint, PDFCraft instead of Acrobat.

On top of that there is a **marketplace**: anyone with an account can add a third-party
open-source project with a single GitHub link — the launcher pulls stars, licence, releases and
the right file for your operating system.

## Features

**Catalog and installation**
- 13 applications with download progress, resume, SHA-256 verification and uninstall
- install folder picker, desktop and Start menu shortcuts, autostart, low-spec mode
- tray icon: closing the window keeps ArteCraft running in the notification area

**Import from GitHub**
- accepts `owner/repo`, a full URL, URLs with `.git` or a branch path
- metadata from the GitHub API: stars, forks, licence, language, activity, archived flag
- release files are read straight from the release page, so the API rate limit cannot block an install
- picks the asset for your OS and architecture and hides service files (symbols, sources, checksums)
- risk screen with a 0–100 score instead of a blind install
- extracts `.zip`, `.tar.gz`, `.deb` (without touching the system package manager), `.AppImage`, `.exe/.msi`, `.dmg`
- downloads are allowed only from `github.com` and `*.githubusercontent.com`, every redirect is checked

**Launch options**
- command-line arguments, working directory, environment variables
- named profiles and a post-install hook

**Updates**
- the launcher asks the server on start and every 30 minutes
- a newer build shows a bar with a download button
- a version that is no longer supported blocks the launcher and the server rejects installs with `409 update_required`

**Interface**
- 15 languages: English, German, French, Spanish, Portuguese, Italian, Dutch, Polish, Russian,
  Japanese, Korean, Chinese, Turkish, Arabic, Hindi
- light and dark theme, Arabic gets right-to-left layout
- native window: WebView2 on Windows, WebKitGTK on Linux — never a browser tab

Screenshots of the current build: [getartecraft.com](https://getartecraft.com) → *Launcher*.

## Install

Download the latest build from **[getartecraft.com/download](https://getartecraft.com/download)**
or take it from the [Releases](https://github.com/getartecraft/artecraft-desktop/releases) page.

| Platform | File | How to run |
|---|---|---|
| Windows 10/11 (x64) | `ArteCraft-2.5.4-Windows-Portable.zip` | Unpack and start `ArteCraft.exe`. Uses the WebView2 Runtime when present, otherwise a chromeless app window of an installed Chromium browser |
| Linux (x64) | `ArteCraft-Linux-x86_64.tar.gz` | `tar xzf`, then `./ArteCraft` |
| Linux (x64) | `ArteCraft-Linux-x86_64.AppImage` | `chmod +x` and run |
| macOS | `ArteCraft-macOS-Universal.zip` | Unpack, open the `.app` (the current package is older than 2.5.4) |

Nothing is installed into the system: the application lives in its own folder and uninstalls by
deleting that folder. User data stays in `%APPDATA%\ArteCraft` (Windows) or `~/.config/artecraft`.

## Build from source

```bash
git clone https://github.com/getartecraft/artecraft-desktop
cd artecraft-desktop/src

# any platform, local server mode (no GTK/WebView dependencies)
CGO_ENABLED=0 go build -trimpath -o ArteCraft .

# Linux with a GTK window (needs libgtk-3-dev, libwebkit2gtk-4.1-dev)
CGO_ENABLED=1 go build -trimpath -o ArteCraft .

# Windows, cross-compiling from Linux (needs mingw-w64)
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -trimpath -ldflags "-s -w -H windowsgui" -o ArteCraft.exe .

# macOS with a WKWebView window (needs Xcode Command Line Tools)
CGO_ENABLED=1 GOOS=darwin go build -trimpath -o ArteCraft .
```

Check the build without opening a window:

```bash
./ArteCraft --self-test
# [PASS] ArteCraft 2.5.4 (linux/amd64) self-test OK | catalog=13 | selfSHA256=…
```

Runs without arguments and serves the UI on `127.0.0.1:<random port>`, then opens the native
window. `--tray` starts hidden in the tray (this is what autostart uses).
Details: [docs/BUILD.md](docs/BUILD.md).

## How it works

The binary is a single Go file tree that serves the UI from `src/ui/` over loopback HTTP and
runs the download, extraction and launch logic behind `POST /api/install`. The window is the
system WebView pointed at that local address; there is no remote UI and no cloud round-trip.

- `src/main.go` — catalog, install worker, HTTP API, autostart, tray
- `src/releases.go` — how the right release file is found (release page → name templates → API)
- `src/market.go` — GitHub import, marketplace proxy, archive extraction
- `src/ui/` — vanilla HTML/CSS/JS, 15 languages, no build step
- `docs/ARCHITECTURE.md` — request flows and data files
- `docs/MARKETPLACE.md` — catalog API and moderation

## Privacy

- no accounts required, no ads, no analytics SDK
- the only network calls go to `getartecraft.com` (update check, marketplace) and `github.com` / `*.githubusercontent.com` (downloads)
- a random install identifier is sent with the update check so the server can answer with the right version; it is not tied to a person
- no file contents, no browsing history, no third-party trackers

## Contributing

Issues and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Please report
security problems privately as described in [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE). You may use, modify and redistribute it, including commercially;
keep the copyright notice.
