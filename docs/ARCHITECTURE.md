# Архитектура

## Процесс
Один Go-бинарник:
1. слушает `127.0.0.1:<случайный порт>`;
2. отдаёт встроенный UI (`go:embed ui/*`) и JSON-API;
3. открывает этот адрес в системном webview (WebView2 / WKWebView / WebKitGTK)
   либо, если cgo выключен, просто печатает адрес — режим «локальный сервер».

## API (локальный)
| Метод | Путь | Назначение |
|---|---|---|
| GET | `/api/status` | каталог, установленное, задачи, настройки, аккаунт |
| POST | `/api/install` | поставить каталожные приложения |
| POST | `/api/launch` | запустить каталожное приложение |
| POST | `/api/uninstall` | удалить |
| GET | `/api/github/preview` | метаданные репозитория + ассеты |
| POST | `/api/import` | скачать и установить выбранный ассет |
| POST | `/api/custom/launch` | запуск импортированного с параметрами |
| GET/POST | `/api/launch-opts` | аргументы, cwd, env, профили, хук |
| GET | `/api/market/catalog` | прокси серверного каталога |
| POST | `/api/market/submit` `/review` `/report` `/suggest` | действия с аккаунтом |
| GET/POST | `/api/site/*` | вход, выход, профиль |
| GET/POST | `/api/settings`, `/api/pick-folder` | настройки лаунчера |

## Состояние
`launcher-state-v2.json` в каталоге приложения: `installRoot`, `installed`, `custom`, `launch`,
`siteToken`, `siteEmail`, `siteHandle`, флаги UI. Импортированные приложения живут в
`<installRoot>/custom/<owner-repo>/`.

## Проверки безопасности
- импорт только с `github.com` / `*.githubusercontent.com` (`isGithubDownloadURL`);
- zip-slip отсекается (`filepath.Clean` + проверка `..`);
- `.deb` распаковывается в каталог приложения, без `dpkg -i` и без root;
- исполняемый файл ищется в дереве, а не берётся первой строкой архива;
- SHA-256 считается по ходу скачивания и сохраняется в состоянии.
