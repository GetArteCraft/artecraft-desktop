# Сборка

## Что нужно
- Go 1.22+ (проверено на 1.23.6)
- Windows: ничего дополнительно (WebView2 входит в Windows 10/11)
- macOS: `xcode-select --install` (окно — WKWebView через cgo)
- Linux: `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev` для окна; без cgo собирается режим локального сервера

## Команды
```bash
cd src
go vet ./... && go test ./...

# без системных библиотек (сервер + браузер)
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ArteCraft .

# с окном на Linux
CGO_ENABLED=1 go build -trimpath -o ArteCraft .

# Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ArteCraft.exe .
```

## Версия
`src/main.go` → `const LauncherVersion = "2.3.0"`.
UI версию не дублирует — берёт из `/api/status`.

## Иконки
- `icon/artecraft.svg` — вектор, `icon/artecraft-1024.png`, `icon/artecraft.ico`
- macOS: `mkdir ArteCraft.iconset` (16…1024 px) → `iconutil -c icns ArteCraft.iconset`

## Подпись
Пока не делается. Windows: Authenticode EV + `signtool sign /fd sha256 /tr http://timestamp.digicert.com ...`.
macOS: Developer ID + `codesign --deep --options runtime` + `xcrun notarytool submit`.

## Локальная отладка против своего сервера
```bash
ARTECRAFT_ORIGIN=http://127.0.0.1:3000 ./ArteCraft
```
