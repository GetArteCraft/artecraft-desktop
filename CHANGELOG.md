# Changelog

## 2.5.4 — 2026-11-03

### Fixed — one window on every Windows PC, working tray
- **The launcher no longer depends on the WebView2 Runtime.** 2.5.3 showed a blank
  "ArteCraft is not responding" window on machines without the runtime and then exited.
  The runtime is now probed in the registry *before* any window is created, and the UI
  is opened in a chromeless app window (`--app`, no address bar, no tabs, one window) of
  an installed Chromium browser — Edge, Chrome, Brave, Vivaldi or Opera.
- **The tray icon works in both modes.** It is created by the ArteCraft process itself,
  so "Open", "Minimize to tray" and "Quit" work even when the UI window belongs to a
  browser process: window show/hide resolves the window by its title. Closing the window
  while "minimize to tray" is on keeps the launcher alive in the tray, and the next
  "Open" reopens the window.
- **"Choose folder" in Settings did nothing.** The Windows implementation was a stub
  returning an empty string; it now opens the system folder picker.
- **Icon.** The launcher window, tray and shortcuts use the ArteCraft mark instead of the
  old four-square placeholder; a `favicon.png` was added for the window UI.

### Notes
- Exactly one ArteCraft process and one visible window; a second start focuses the
  existing window and exits in a few milliseconds.
- Defaults unchanged: minimize to tray ON, autostart ON, settings survive restarts.

## 2.5.3 — 2026-11-03

### Fixed — tray and the "two windows" bug (root causes)
- **The tray icon is a real window now.** The message sink for `Shell_NotifyIcon` was
  created as a message-only window (`HWND_MESSAGE`); Windows does not deliver shell
  notifications to such windows, so the icon never appeared. It is a hidden popup
  window now, and the icon is re-added automatically after an Explorer restart
  (`TaskbarCreated`).
- **The tray window no longer borrows the WebView2 window's handler.** Unhandled
  messages of the sink were forwarded with `CallWindowProc` to the previous window
  procedure of a *different* window, which could freeze the UI.
- **No more second window.** When WebView2 failed, `runPlatformDesktopUI` additionally
  launched Edge/Chrome in app mode — two windows appeared: a blank "not responding"
  one and a browser one. The browser path is removed; on failure the app shows a native
  message about the missing WebView2 Runtime and exits.
- **WebView2 self-heals.** Three attempts with different data folders
  (`WebView2Data` → `WebView2Data-clean` → temp), each logged.
- **Settings migration.** One-time `settingsRev` migration forces `closeToTray` and
  `autoStart` back to `true` for everyone who had them saved as `false` by older builds.
- `staticcheck` bug: removed a no-op self-assignment in the CLI install path.

### Added
- `launcher.log` (rotated, 512 KB) and `--diagnose` output now include tray state and
  the exact WebView2 data folder used.

## 2.5.2 — 2026-11-03

### Fixed
- **Regression of 2.5.1: a second start could open a second blank window**
  ("ArteCraft — not responding"). Two WebView2 instances shared one data folder, so the
  second one never rendered. The single-instance lock is now a kernel object
  (named mutex on Windows, `flock` on Unix) — the OS guarantees only one process,
  no PID heuristics, no lock stealing while the first instance is still starting.
- **Tray icon is created always.** Previously it was installed only when "minimise to
  tray" was enabled at startup; if the setting was off, the icon was missing and there
  was no way to bring the window back. Now the icon is always there, the option only
  controls what the close button does.
- Second start now waits up to 6 seconds for the running instance (it may still be
  starting) and brings its window up via `/api/app/show`, falling back to
  `FindWindow` + `ShowWindow` on Windows. Never a browser tab.

### Added
- `--diagnose` flag: prints version, app data dir, state and log paths, tray support.
- File log `launcher.log` (rotated at 512 KB) in the app data folder — needed because a
  windowless Windows build has nowhere to print to.

## 2.5.1 — 2026-11-03

### Fixed
- **Only one instance runs.** Starting the launcher again brings the existing window
  back from the tray. Before, the second start opened the previous instance in the
  default browser, which looked like "the launcher does not open" and pushed people to
  start it again — several processes, several tray icons, lost settings.
- **Settings survive restarts and crashes.** State is written atomically
  (temp file + fsync + rename) with a `.bak` fallback that is used automatically when
  the main file is corrupted. Previously a plain overwrite could truncate the JSON, so
  language, install folder, tray and autostart options appeared not to be saved.
- Closing the window while auto-started with `--tray` no longer leaves an invisible process.
- Quitting the app now flushes settings to disk before exit.

### Changed
- Windows binary carries real file metadata (ProductName ArteCraft, FileVersion 2.5.1.0,
  MIT) and an application manifest: `asInvoker`, per-monitor-v2 DPI, UTF-8 code page.
  This also reduces antivirus false positives on unsigned Go binaries.
- New endpoint `/api/app/show` used by the second start and by desktop shortcuts.

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
