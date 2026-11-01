# Contributing

Thanks for taking a look. Bug reports, translations and small fixes are all welcome.

## Before you start

- read `docs/ARCHITECTURE.md` to see where the code lives
- for anything larger than a bug fix, open an issue first and describe the idea
- one change per pull request, please

## Code

- Go 1.22+, `gofmt` clean, `go vet ./...` and `go test ./...` must pass
- user-facing text goes through the i18n keys in `src/ui/i18n.js` — never a literal string in Go
  or in the HTML; add the key to all 15 languages
- do not break the download guard: assets may only come from `github.com`, `api.github.com` and
  `*.githubusercontent.com`
- keep the platform-specific code behind the `autostart_*.go` / `platform_*.go` build tags

## Testing a build

```bash
cd src
go vet ./... && go test ./...
CGO_ENABLED=0 go build -trimpath -o ArteCraft . && ./ArteCraft --self-test
```

## Commits and pull requests

- write the commit subject in the imperative mood: `fix: keep scroll position on refresh`
- describe what you changed and how you tested it
- mention the platform you tested on (Windows, Linux, macOS)

## Translations

Each key in `src/ui/i18n.js` has an array of 15 strings in this order: en, de, fr, es, pt, it, nl,
pl, ru, ja, ko, zh, tr, ar, hi. If a translation is wrong, change only that slot and say which
language you are fixing.
