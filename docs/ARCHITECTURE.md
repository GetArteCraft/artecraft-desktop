# Architecture

ArteCraft is one Go binary with an embedded web UI. There is no remote frontend and no database:
state is a single JSON file in the user profile.

```
main.go        catalog, install worker, HTTP API, autostart, tray
releases.go    resolving the download URL of a release
market.go      GitHub import, marketplace proxy, archive extraction
messages.go    localisable error type (i18n key + technical detail)
autostart_*.go per-platform autostart (registry / XDG desktop file / LaunchAgent)
platform_*.go  window, shortcuts, "open folder", native dialogs
tray_*.go      tray icon and window hide/show
ui/            index.html, app.js, styles.css, i18n.js, seeddesc.js
```

## Request flow

1. The binary starts an HTTP server on `127.0.0.1` with a random free port and writes the address
   to the instance file. A second launch detects it and reuses the running instance.
2. The native window opens that address. The UI is plain HTML/CSS/JS, no bundler.
3. The UI polls `GET /api/status` every 1.5 seconds: version, installed apps, tasks, release map,
   update state, settings.
4. `POST /api/install` queues slugs into a channel; one worker downloads, checks SHA-256,
   extracts and records the app. Progress is reported through the task map read by `/api/status`.
5. `POST /api/import` takes a repository and either uses the asset the UI picked or resolves one
   itself, then runs the same install path.

## Resolving a release file

Order of attempts, first success wins:

1. `https://github.com/<repo>/releases/expanded_assets/<tag>` — HTML page, no API limit
2. generated file names for the current OS and architecture, checked with `HEAD`
3. `https://api.github.com/repos/<repo>/releases/latest` (60 requests per hour without a token)
4. the last known good entry in the local release cache

Every URL, including each redirect hop, must pass `isAllowedDownloadURL`: HTTPS and a host of
`github.com`, `api.github.com` or `*.githubusercontent.com`.

## Update channel

`GET /api/launcher/check?cid=<id>&v=<version>` answers with `latestVersion`, `minVersion`,
`forceUpdate`, `forceBlockAll`, `notifyBanner`, a download URL and localised title/message.

- newer version, no force → banner with a download button
- `minVersion` above the running build, or `forceBlockAll` → locked mode: the UI shows a full
  overlay and `POST /api/install` / `POST /api/import` answer `409 err.updateRequired`

## Files on disk

| Path | Purpose |
|---|---|
| `%APPDATA%\ArteCraft\state.json` (Windows) / `~/.config/artecraft/state.json` | settings, installed apps, import cache |
| `…/release-cache.json` | last resolved release per catalog entry, 6-hour TTL |
| `…/instance` | port of the running instance |
| install root (default `~/.local/opt/artecraft`, `%LOCALAPPDATA%\ArteCraft`) | extracted applications |

## Localisation

Every user-facing string is a key in `ui/i18n.js` with 15 translations. Go code never sends
ready-made text: errors and task stages travel as keys (`err.hostBlocked`, `task.extracting`) and
the UI translates them. A test fails the build if Cyrillic appears in Go string literals or in the
HTML, so a language cannot leak into another locale.
