# Changelog

All notable changes to ArteCraft Desktop Hub. Versions follow [SemVer](https://semver.org).

## 2.5.0 — 2026-11-01

Clean release for posting: no leftovers from earlier builds, one naming everywhere
(`ArteCraft`, repository `artecraft-desktop`, version `2.5.0`).

### Added
- **Update channel with a real lock.** The launcher asks the server on start and then every
  30 minutes. When a newer build exists, a bar appears at the top with a download button.
  When the build is no longer supported (`minVersion` / `forceBlockAll` on the server), the
  window is blocked and `POST /api/install` and `POST /api/import` answer `409 err.updateRequired`
  — the check happens server-side, so it cannot be bypassed from the console.
- **Release assets are read from the release page**, without the GitHub API and without the
  60-requests-per-hour limit (`/releases/expanded_assets/<tag>`). The API is now only the third
  fallback.

### Changed
- **Tray and autostart are on by default.** Windows writes
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` with `--tray`; Linux writes
  `~/.config/autostart/artecraft.desktop`; macOS uses a LaunchAgent. An explicit `false` in
  `state.json` is respected and never overwritten.
- **Asset picking by OS and architecture.** arm32/armhf/i686/win32 builds are rejected,
  x86_64/amd64 is preferred, and portable archives (AppImage, tar.gz, zip) rank above
  msi/exe/deb/rpm.
- **No console windows on Windows.** The registry is accessed natively through
  `golang.org/x/sys/windows/registry` (no `reg.exe`), the autostart state is cached for 30 s,
  and every helper process runs with `HideWindow` + `CREATE_NO_WINDOW`.

### Fixed
- Install from the marketplace failed with *"Only GitHub links are supported"*, even for the
  official entries: the preview response had no `downloadUrl` / `assetName` / `version` fields,
  so the UI posted an empty URL. The fields are there now, and the server resolves the file
  itself when the UI has none.
- Shortcuts: the "Start menu" option was ignored, only the desktop link was created.
- Scrolling works in every tab and on the sign-in / registration screens: the view gets
  `min-height: 0`, the scroll position survives the 1.5 s refresh, and the wheel over a
  select no longer changes the value.

## 2.4.0 — 2026-11-01

### Added
- Interface in 15 languages (`src/ui/i18n.js`): en, de, fr, es, pt, it, nl, pl, ru, ja, ko,
  zh, tr, ar, hi, with RTL for Arabic and a language switcher in the header and settings.
- Install dialog with options: target folder (absolute path + write check), desktop shortcut,
  Start menu entry, launch after install, launch arguments, SHA-256 verification.
- `POST /api/check-dir` — folder validation before extraction starts.
- Diagnostics in settings: install folder, network, account, one-click shortcut rebuild.
- Application icon: `src/app.ico` + `src/rsrc_windows_amd64.syso`, embedded into the `.exe`.

### Changed
- No hard-coded UI strings: markup uses `data-i18n`, text comes from `t('key')`.
- Modals: header and footer are pinned, only the body scrolls.

## 2.3.0 — 2026-10-31

### Added
- Marketplace: server catalog, OS filters, support counters, "verified" and "official" marks.
- Import from any GitHub repository: preview of stars, license, release notes and assets.

## 2.0.0 — 2026-10-28

### Added
- Native window (WebView2 on Windows, WebKitGTK on Linux), no browser tab.
- Catalog of 13 desktop applications with download, extract, launch and uninstall.
- Install root selection, library view, downloads view, import from GitHub.

## 1.0.0 — 2026-10-20

- First public build: local HTTP UI, catalog list, download and launch of a single app.
