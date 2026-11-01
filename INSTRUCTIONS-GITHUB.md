# Как залить репозиторий на GitHub

В архиве — готовый репозиторий: `README.md` с бейджами и скриншотами, `LICENSE` (MIT),
`CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`, GitHub Actions
(тесты + сборки под Windows/macOS/Linux) и сам исходник в `src/`.

---

## Вариант A — через веб (без установки программ)

1. Откройте <https://github.com/new>.
2. **Repository name**: `artecraft-desktop` (владелец — ваш аккаунт).
3. **Description**: `ArteCraft Desktop Hub — 13 open-source apps + GitHub marketplace in one launcher`.
4. Public, **без** галочек «Add a README», «Add .gitignore», «Choose a license» — они уже есть в архиве.
5. **Create repository** → на странице пустого репозитория нажмите **«uploading an existing file»**.
6. Распакуйте архив и перетащите **содержимое** папки (не саму папку) в окно загрузки:
   `README.md`, `LICENSE`, `src/`, `docs/`, `icon/`, `scripts/`, `.github/`, `.gitignore` и остальные файлы.
7. Внизу — Commit changes, сообщение: `feat: ArteCraft Launcher 2.3.0`.
8. Проверьте, что вкладка **Actions** зелёная: workflow `build` соберёт три платформы.

> Важно: папка `.github` начинается с точки — в проводнике Windows включите
> «Показать расширения и скрытые элементы», иначе она не отобразится.

### Загрузить папку целиком (если файлов много)
На странице репозитория: **Add file → Upload files** → перетащите папку `src` целиком.
GitHub сохранит структуру. Повторите для `docs`, `icon`, `scripts`, `.github`.

---

## Вариант B — GitHub Desktop (удобнее для правок)

1. Скачайте <https://desktop.github.com> и войдите в аккаунт.
2. **File → New repository…**
   - Name: `artecraft-launcher`
   - Local path: любая папка
   - Initialize with README: **снять галочку**
   - License: **None** (LICENSE уже в архиве)
3. Нажмите **Create repository**, затем **Show in Explorer / Finder** — откроется пустая папка.
4. Скопируйте туда **всё содержимое** архива (файлы + папки, включая `.github`).
5. В GitHub Desktop появятся изменения → внизу слева поле Summary: `ArteCraft Launcher 2.3.0`
   → **Commit to main**.
6. Сверху **Publish repository**: снимите «Keep this code private» (если нужен публичный) → **Publish**.
7. Дальше любые правки: изменили файл → Summary → Commit → **Push origin**.

---

## Вариант C — терминал

```bash
cd artecraft-launcher
git init -b main
git add .
git commit -m "feat: ArteCraft Launcher 2.3.0"
git remote add origin https://github.com/<you>/artecraft-launcher.git
git push -u origin main
```

Со своим SSH-ключом:
```bash
git remote add origin git@github.com:<you>/artecraft-launcher.git
```

---

## После заливки

**Релиз с файлами.** Releases → **Draft a new release** → tag `v2.3.0` → title `ArteCraft Launcher 2.3.0`
→ перетащите `ArteCraft.exe`, `ArteCraft-Linux-x86_64`, `ArteCraft-macOS-Universal.zip`
→ Publish. Ссылки вида `https://github.com/<you>/artecraft-launcher/releases/download/v2.3.0/ArteCraft.exe`.

**Описание и сайт.** About (справа) → Description + Website `https://getartecraft.com` + Topics:
`go`, `desktop`, `launcher`, `marketplace`, `open-source`, `windows`, `macos`, `linux`, `webview2`, `wkwebview`.

**Соцпревью.** Settings → Social preview → загрузите `icon/artecraft-1024.png`.

**Ветки и защита.** Settings → Branches → Add branch protection rule → `main` → Require pull request
перед merge, если работаете не один.

**Issues.** Шаблоны уже лежат в `.github/ISSUE_TEMPLATE` — появятся автоматически на вкладке Issues.

**Actions.** Сборки запускаются на push в `main` и на теги `v*`. Артефакты — в разделе Actions → нужный run.
