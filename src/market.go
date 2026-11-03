package main

// market.go — импорт стороннего софта с GitHub, каталог маркетплейса и
// пользовательские настройки запуска.
//
// Модель безопасности: приложение ничего не «ищет само» и не запускает код
// без явного действия пользователя. Импорт = ссылка на GitHub-репозиторий →
// предпросмотр метаданных через GitHub API → пользователь выбирает ассет под
// свою ОС → подтверждает экран риска → скачивание, SHA-256, распаковка, запись
// в состояние. Все проверки показываются в UI, но не блокируют установку:
// фильтры (звёзды/лицензия/свежесть) — это рекомендации, а не запреты.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- модели ---------------------------------------------------------

type CustomApp struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Repo          string   `json:"repo"`
	Description   string   `json:"description,omitempty"`
	Version       string   `json:"version"`
	AssetName     string   `json:"assetName"`
	AssetURL      string   `json:"assetUrl"`
	ExecPath      string   `json:"execPath"`
	InstallFolder string   `json:"installFolder"`
	SizeMb        float64  `json:"sizeMb"`
	Sha256        string   `json:"sha256,omitempty"`
	InstalledAt   string   `json:"installedAt"`
	Category      string   `json:"category,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Stars         int      `json:"stars"`
	License       string   `json:"license,omitempty"`
	PushedAt      string   `json:"pushedAt,omitempty"`
	// пользовательские пометки
	Note      string `json:"note,omitempty"`
	Untrusted bool   `json:"untrusted"`
}

type LaunchConfig struct {
	Args        string            `json:"args"`
	Cwd         string            `json:"cwd"`
	Env         map[string]string `json:"env,omitempty"`
	PostInstall string            `json:"postInstall,omitempty"`
	Profiles    []LaunchProfile   `json:"profiles,omitempty"`
}

type LaunchProfile struct {
	Name string `json:"name"`
	Args string `json:"args"`
	Cwd  string `json:"cwd"`
}

type GithubAsset struct {
	Name    string  `json:"name"`
	URL     string  `json:"url"`
	SizeMb  float64 `json:"sizeMb"`
	OS      string  `json:"os"`
	Kind    string  `json:"kind"`
	Primary bool    `json:"primary"`
	Service bool    `json:"service,omitempty"`
}

type GithubPreview struct {
	Repo        string        `json:"repo"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	HTMLURL     string        `json:"htmlUrl"`
	Stars       int           `json:"stars"`
	Forks       int           `json:"forks"`
	License     string        `json:"license"`
	PushedAt    string        `json:"pushedAt"`
	CreatedAt   string        `json:"createdAt"`
	Archived    bool          `json:"archived"`
	Language    string        `json:"language"`
	Topics      []string      `json:"topics"`
	ReleaseTag  string        `json:"releaseTag"`
	ReleaseDate string        `json:"releaseDate"`
	ReleaseNote string        `json:"releaseNote"`
	Assets      []GithubAsset `json:"assets"`
	// Эти поля читает интерфейс перед установкой. Раньше их не было в структуре,
	// JSON приходил без downloadUrl/assetName, UI отправлял пустой assetUrl —
	// и сервер отвечал «Only GitHub links are supported» даже для официальных
	// приложений. Теперь ссылка на файл и имя ассета реально уходят в ответ.
	Version     string   `json:"version"`
	DownloadURL string   `json:"downloadUrl"`
	AssetName   string   `json:"assetName"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Warnings    []string `json:"warnings"`
	WarningKeys []string `json:"warningKeys"`
	Score       int      `json:"score"`
}

// ---------- вспомогательное ------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func githubToken() string {
	mu.Lock()
	defer mu.Unlock()
	if env := strings.TrimSpace(os.Getenv("ARTECRAFT_GITHUB_TOKEN")); env != "" {
		return env
	}
	return strings.TrimSpace(appState.GithubToken)
}

func githubRequest(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ArteCraft-Launcher/"+LauncherVersion)
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 404 {
		return uerr("err.repoNotFound", "")
	}
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return uerr("err.rateLimit", "")
	}
	if resp.StatusCode != 200 {
		return errHTTP(resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NormalizeRepoURL принимает что угодно: owner/name, https://github.com/owner/name,
// с .git, с /tree/main, со ссылкой на релиз. Возвращает "owner/name".
func NormalizeRepoURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", uerr("err.emptyLink", "")
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", uerr("err.badLink", "")
		}
		host := strings.ToLower(u.Host)
		if host != "github.com" && host != "www.github.com" {
			return "", uerr("err.onlyGithub", "")
		}
		s = strings.TrimPrefix(u.Path, "/")
	} else {
		s = strings.TrimPrefix(s, "github.com/")
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", uerr("err.repoFormat", "")
	}
	return parts[0] + "/" + parts[1], nil
}

func assetKind(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, ".dmg"):
		return "dmg"
	case strings.HasSuffix(low, ".msi"):
		return "msi"
	case strings.HasSuffix(low, ".exe"):
		return "exe"
	case strings.HasSuffix(low, ".appimage"):
		return "appimage"
	case strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz"):
		return "targz"
	case strings.HasSuffix(low, ".deb"):
		return "deb"
	case strings.HasSuffix(low, ".rpm"):
		return "rpm"
	case strings.HasSuffix(low, ".pkg.tar.zst"):
		return "archpkg"
	case strings.HasSuffix(low, ".zip"):
		return "zip"
	}
	return "other"
}

func assetOS(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "windows") || strings.Contains(low, "win64") || strings.Contains(low, "win32") ||
		strings.HasSuffix(low, ".exe") || strings.HasSuffix(low, ".msi"):
		return "windows"
	case strings.Contains(low, "macos") || strings.Contains(low, "darwin") || strings.Contains(low, "osx") ||
		strings.Contains(low, "universal") || strings.HasSuffix(low, ".dmg"):
		return "macos"
	case strings.Contains(low, "linux") || strings.Contains(low, "ubuntu") || strings.Contains(low, "debian") ||
		strings.Contains(low, "fedora") || strings.Contains(low, "appimage") || strings.HasSuffix(low, ".deb") ||
		strings.HasSuffix(low, ".rpm") || strings.Contains(low, ".appimage"):
		return "linux"
	case strings.Contains(low, "freebsd"):
		return "freebsd"
	}
	return "any"
}

func currentOSKey() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// ScorePreview считает «здоровье» репозитория — то, что видит пользователь
// в экране риска. Не блокирует установку, только подсвечивает.
func ScorePreview(p *GithubPreview) int {
	score := 0
	switch {
	case p.Stars >= 5000:
		score += 45
	case p.Stars >= 1000:
		score += 35
	case p.Stars >= 200:
		score += 25
	case p.Stars >= 50:
		score += 15
	case p.Stars > 0:
		score += 5
	}
	if p.License != "" {
		score += 15
	}
	if t, err := time.Parse(time.RFC3339, p.PushedAt); err == nil {
		age := time.Since(t).Hours() / 24
		switch {
		case age < 90:
			score += 25
		case age < 365:
			score += 15
		case age < 900:
			score += 5
		}
	}
	if p.ReleaseTag != "" {
		score += 10
	}
	if p.Archived {
		score -= 25
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// ---------- привязка к железу ---------------------------------------------

var (
	deviceOnce sync.Once
	deviceVal  string
)

// rawDeviceFingerprint читает стабильный идентификатор машины средствами ОС.
// Это не «защита от всего», а честная привязка: один компьютер — один отпечаток.
func rawDeviceFingerprint() string {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("reg", "query", `HKLM\\SOFTWARE\\Microsoft\\Cryptography`, "/v", "MachineGuid").Output()
		if err == nil {
			fields := strings.Fields(string(out))
			if len(fields) > 0 {
				return "win:" + fields[len(fields)-1]
			}
		}
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "IOPlatformUUID") {
					parts := strings.Split(line, "\"")
					if len(parts) >= 2 {
						return "mac:" + parts[len(parts)-2]
					}
				}
			}
		}
	default:
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id", "/sys/class/dmi/id/product_uuid"} {
			if b, err := os.ReadFile(p); err == nil {
				if v := strings.TrimSpace(string(b)); v != "" {
					return "linux:" + v
				}
			}
		}
	}
	// запасной вариант: постоянный случайный id в appdata
	fallback := filepath.Join(getAppDataDir(), "device-id")
	if b, err := os.ReadFile(fallback); err == nil && len(strings.TrimSpace(string(b))) > 8 {
		return "fallback:" + strings.TrimSpace(string(b))
	}
	val := randomHex(16)
	_ = os.MkdirAll(getAppDataDir(), 0755)
	_ = os.WriteFile(fallback, []byte(val), 0600)
	return "fallback:" + val
}

// DeviceID — хеш отпечатка машины. Одинаков между запусками и версиями лаунчера.
func DeviceID() string {
	deviceOnce.Do(func() {
		sum := sha256.Sum256([]byte("artecraft-launcher|" + rawDeviceFingerprint()))
		deviceVal = hex.EncodeToString(sum[:])
	})
	return deviceVal
}

// ---------- обработчики ----------------------------------------------------

func registerMarketRoutes(mux *http.ServeMux) {
	// Предпросмотр репозитория (метаданные + релизы + ассеты под ОС)
	mux.HandleFunc("/api/github/preview", func(w http.ResponseWriter, r *http.Request) {
		repo, err := NormalizeRepoURL(r.URL.Query().Get("repo"))
		if err != nil {
			key, detail := errKeyOf(err)
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"errorKey": key, "errorDetail": detail})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()

		var repoResp struct {
			FullName    string   `json:"full_name"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			HTMLURL     string   `json:"html_url"`
			Stars       int      `json:"stargazers_count"`
			Forks       int      `json:"forks_count"`
			PushedAt    string   `json:"pushed_at"`
			CreatedAt   string   `json:"created_at"`
			Archived    bool     `json:"archived"`
			Language    string   `json:"language"`
			Topics      []string `json:"topics"`
			License     *struct {
				SPDX string `json:"spdx_id"`
				Name string `json:"name"`
			} `json:"license"`
		}
		if err := githubRequest(ctx, "https://api.github.com/repos/"+repo, &repoResp); err != nil {
			key, detail := errKeyOf(err)
			// Лимит GitHub API (60 запросов в час на IP) не должен ломать установку:
			// берём минимум из адреса репозитория и идём дальше — файлы релиза
			// найдём по HTML-странице, где лимита нет.
			if key != "err.rateLimit" {
				w.WriteHeader(http.StatusBadGateway)
				writeJSON(w, map[string]any{"errorKey": key, "errorDetail": detail})
				return
			}
			repoResp.FullName = repo
			repoResp.Name = repo
			if i := strings.LastIndex(repo, "/"); i >= 0 && i+1 < len(repo) {
				repoResp.Name = repo[i+1:]
			}
		}

		p := GithubPreview{
			Repo:        repoResp.FullName,
			Name:        repoResp.Name,
			Description: repoResp.Description,
			HTMLURL:     repoResp.HTMLURL,
			Stars:       repoResp.Stars,
			Forks:       repoResp.Forks,
			PushedAt:    repoResp.PushedAt,
			CreatedAt:   repoResp.CreatedAt,
			Archived:    repoResp.Archived,
			Language:    repoResp.Language,
			Topics:      repoResp.Topics,
		}
		if repoResp.License != nil {
			p.License = repoResp.License.SPDX
			if p.License == "" || p.License == "NOASSERTION" {
				p.License = repoResp.License.Name
			}
		}

		var rel struct {
			TagName string `json:"tag_name"`
			Body    string `json:"body"`
			PubAt   string `json:"published_at"`
			Assets  []struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
				Size int64  `json:"size"`
			} `json:"assets"`
		}
		releaseFromAPI := githubRequest(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", &rel) == nil
		if !releaseFromAPI {
			// API недоступен (лимит, сеть, приватный релиз) — тянем список файлов
			// со страницы https://github.com/{repo}/releases/expanded_assets/{tag}.
			if tag := resolveLatestTag(repo); tag != "" {
				p.ReleaseTag = tag
				for _, a := range assetsFromExpandedPage(repo, tag) {
					p.Assets = append(p.Assets, a)
				}
			}
		}
		if releaseFromAPI {
			p.ReleaseTag = rel.TagName
			p.ReleaseDate = rel.PubAt
			p.ReleaseNote = truncate(rel.Body, 1200)
			for _, a := range rel.Assets {
				kind := assetKind(a.Name)
				low := strings.ToLower(a.Name)
				service := false
				for _, bad := range []string{"dsym", "sources", "debug", "dbgsym", "ddeb", "checksum", "sha256", ".sig", ".asc", "symbols"} {
					if strings.Contains(low, bad) {
						service = true
					}
				}
				p.Assets = append(p.Assets, GithubAsset{
					Name:    a.Name,
					URL:     a.URL,
					SizeMb:  float64(a.Size) / (1024 * 1024),
					OS:      assetOS(a.Name),
					Kind:    kind,
					Service: service,
				})
			}
		}

		// Отмечаем лучший ассет под текущую ОС и архитектуру: та же логика,
		// что и в releases.go (одна реализация на весь проект).
		best := pickBestAsset(p.Assets)
		if best >= 0 {
			p.Assets[best].Primary = true
			p.DownloadURL = p.Assets[best].URL
			p.AssetName = p.Assets[best].Name
		}
		p.Version = strings.TrimPrefix(strings.TrimSpace(p.ReleaseTag), "v")
		p.Tags = p.Topics
		if p.Category == "" {
			p.Category = "utility"
		}

		// Предупреждения для экрана риска
		if p.Assets == nil || len(p.Assets) == 0 {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.noAssets")
		} else if best < 0 {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.noOsAsset")
		}
		if p.License == "" {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.noLicense")
		}
		if p.Archived {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.archived")
		}
		if t, err := time.Parse(time.RFC3339, p.PushedAt); err == nil && time.Since(t).Hours() > 24*500 {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.stale")
		}
		if p.Stars < 50 {
			p.Warnings = append(p.Warnings, "")
			p.WarningKeys = append(p.WarningKeys, "warn.fewStars")
		}
		p.Warnings = append(p.Warnings, "")
		p.WarningKeys = append(p.WarningKeys, "warn.unverified")
		p.Score = ScorePreview(&p)

		if len(p.ReleaseNote) > 0 {
			p.ReleaseNote = strings.ReplaceAll(p.ReleaseNote, "\r\n", "\n")
		}
		writeJSON(w, p)
	})

	// Установка выбранного ассета
	mux.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Repo      string       `json:"repo"`
			AssetURL  string       `json:"assetUrl"`
			AssetName string       `json:"assetName"`
			Version   string       `json:"version"`
			Name      string       `json:"name"`
			Category  string       `json:"category"`
			Tags      []string     `json:"tags"`
			Note      string       `json:"note"`
			Launch    LaunchConfig `json:"launch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"errorKey": "err.badRequest"})
			return
		}
		repo, err := NormalizeRepoURL(req.Repo)
		if err != nil {
			key, detail := errKeyOf(err)
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"errorKey": key, "errorDetail": detail})
			return
		}
		if locked, st := launcherLockedForUpdates(); locked {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"ok": false, "errorKey": "err.updateRequired", "selfUpdate": st})
			return
		}
		if !isGithubDownloadURL(req.AssetURL) {
			// UI приходит с пустым assetUrl, когда GitHub API ответил 403/429 (лимит
			// 60 запросов в час). Раньше это выдавало «Only GitHub links are supported»
			// даже для официальных приложений. Теперь файл релиза ищем сами.
			rel, ok := resolveAssetForRepo(repo)
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				writeJSON(w, map[string]any{"errorKey": "err.assetMissing", "errorDetail": repo})
				return
			}
			req.AssetURL = rel.DownloadURL
			if strings.TrimSpace(req.AssetName) == "" {
				req.AssetName = rel.AssetName
			}
			if strings.TrimSpace(req.Version) == "" {
				req.Version = rel.Version
			}
		}
		slug := slugFromRepo(repo)
		if _, exists := findCustom(slug); exists {
			slug = slug + "-2"
		}
		updateTask(slug, func(t *TaskProgress) {
			t.Stage = "queued"
			t.Message = ""
			t.MessageKey = "task.preparing"
			t.Error = ""
			t.ErrorKey = ""
			t.Percent = 0
		})

		app, err := installCustomApp(r.Context(), importRequest{
			Slug: slug, Repo: repo, AssetURL: req.AssetURL, AssetName: req.AssetName,
			Version: req.Version, Name: req.Name, Category: req.Category,
			Tags: req.Tags, Note: req.Note, Launch: req.Launch,
		})
		if err != nil {
			key, detail := errKeyOf(err)
			updateTask(slug, func(t *TaskProgress) {
				t.Stage = "error"
				t.Error = ""
				t.Message = ""
				t.ErrorKey = key
				t.ErrorDetail = detail
			})
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"errorKey": key, "errorDetail": detail})
			return
		}
		updateTask(slug, func(t *TaskProgress) {
			t.Stage = "installed"
			t.Percent = 100
			t.Message = ""
			t.MessageKey = "task.done"
		})
		sendTelemetry("custom_import", slug)
		writeJSON(w, map[string]any{"ok": true, "app": app})
	})

	mux.HandleFunc("/api/custom/remove", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug  string `json:"slug"`
			Purge bool   `json:"purge"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		app, ok := appState.Custom[body.Slug]
		if ok {
			if body.Purge && app.InstallFolder != "" && strings.HasPrefix(app.InstallFolder, appState.InstallRoot) {
				_ = os.RemoveAll(app.InstallFolder)
			}
			delete(appState.Custom, body.Slug)
			delete(appState.Launch, body.Slug)
			saveStateLocked()
		}
		mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"errorKey": "err.notFound"})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// Запуск с пользовательскими параметрами
	mux.HandleFunc("/api/custom/launch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug    string `json:"slug"`
			Profile string `json:"profile"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		app, ok := appState.Custom[body.Slug]
		cfg := appState.Launch[body.Slug]
		mu.Unlock()
		if !ok || app.ExecPath == "" {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"errorKey": "err.notInstalled"})
			return
		}
		args := splitArgs(cfg.Args)
		cwd := cfg.Cwd
		for _, pr := range cfg.Profiles {
			if pr.Name == body.Profile {
				if strings.TrimSpace(pr.Args) != "" {
					args = splitArgs(pr.Args)
				}
				if strings.TrimSpace(pr.Cwd) != "" {
					cwd = pr.Cwd
				}
			}
		}
		if err := startWithConfig(app.ExecPath, args, cwd, cfg.Env); err != nil {
			key, detail := errKeyOf(err)
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"errorKey": key, "errorDetail": detail})
			return
		}
		sendTelemetry("custom_launch", body.Slug)
		writeJSON(w, map[string]any{"launched": app.ExecPath, "args": args})
	})

	// Настройки запуска (для любого приложения — и каталожного, и импортированного)
	mux.HandleFunc("/api/launch-opts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			slug := r.URL.Query().Get("slug")
			mu.Lock()
			cfg, ok := appState.Launch[slug]
			mu.Unlock()
			if !ok {
				cfg = LaunchConfig{Env: map[string]string{}}
			}
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			writeJSON(w, cfg)
			return
		}
		var body struct {
			Slug           string       `json:"slug"`
			Config         LaunchConfig `json:"config"`
			RunPostInstall bool         `json:"runPostInstall"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Slug == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		if appState.Launch == nil {
			appState.Launch = map[string]LaunchConfig{}
		}
		appState.Launch[body.Slug] = body.Config
		saveStateLocked()
		inst := appState.Installed[body.Slug]
		custom := appState.Custom[body.Slug]
		mu.Unlock()

		// Пост-инсталл хук выполняется только по явной просьбе (кнопка в UI)
		if body.RunPostInstall && strings.TrimSpace(body.Config.PostInstall) != "" {
			dir := inst.InstallFolder
			if dir == "" {
				dir = custom.InstallFolder
			}
			go runHook(body.Config.PostInstall, dir, body.Slug)
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// Маркетплейс: каталог с сайта (серверный, общий для всех пользователей)
	mux.HandleFunc("/api/market/catalog", func(w http.ResponseWriter, r *http.Request) {
		origin := candidateOrigins()[0]
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		target := origin + "/api/market/catalog"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		req.Header.Set("User-Agent", "ArteCraft-Launcher/"+LauncherVersion)
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, map[string]any{"errorKey": "err.catalogUnavailable", "items": []any{}})
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.Copy(w, resp.Body)
	})

	// Отправка своей записи в общий каталог и отзыв — проксируем на сайт с токеном пользователя
	proxyToSite := func(path, event string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			mu.Lock()
			token := appState.SiteToken
			mu.Unlock()
			if token == "" {
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]any{"errorKey": "err.loginRequired"})
				return
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, candidateOrigins()[0]+path, strings.NewReader(string(body)))
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("User-Agent", "ArteCraft-Launcher/"+LauncherVersion)
			resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				writeJSON(w, map[string]any{"errorKey": "err.siteUnavailable"})
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == 200 {
				sendTelemetry(event, "")
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		}
	}
	mux.HandleFunc("/api/market/submit", proxyToSite("/api/market/submit", "market_submit"))
	mux.HandleFunc("/api/market/review", proxyToSite("/api/market/review", "market_review"))
	mux.HandleFunc("/api/market/report", proxyToSite("/api/market/report", "market_report"))
	mux.HandleFunc("/api/market/suggest", proxyToSite("/api/market/suggest", "market_suggest"))

	// Аккаунт на сайте (вход/выход/профиль) — токен хранится в состоянии
	mux.HandleFunc("/api/site/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Register bool   `json:"register"`
			Name     string `json:"name"`
			Handle   string `json:"handle"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		path := "/api/auth/login"
		if body.Register {
			path = "/api/auth/register"
		}
		payload, _ := json.Marshal(map[string]string{
			"email":    body.Email,
			"password": body.Password,
			"name":     body.Name,
			"handle":   body.Handle,
			"login":    body.Email,
			"deviceId": DeviceID(),
		})
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, candidateOrigins()[0]+path, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ArteCraft-Device", DeviceID())
		req.Header.Set("X-ArteCraft-OS", runtime.GOOS)
		resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, map[string]any{"errorKey": "err.siteUnavailable"})
			return
		}
		defer func() { _ = resp.Body.Close() }()
		var out struct {
			Token    string   `json:"token"`
			Email    string   `json:"email"`
			Name     string   `json:"name"`
			Handle   string   `json:"handle"`
			Warnings []string `json:"warnings"`
			Error    string   `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != 200 || out.Token == "" {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"errorKey": "err.loginFailed", "errorDetail": out.Error})
			return
		}
		mu.Lock()
		appState.SiteToken = out.Token
		appState.SiteEmail = out.Email
		appState.SiteName = out.Name
		appState.SiteHandle = out.Handle
		saveStateLocked()
		mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "email": out.Email, "name": out.Name, "handle": out.Handle, "warnings": out.Warnings})
	})

	mux.HandleFunc("/api/site/me", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		writeJSON(w, map[string]any{
			"signedIn": appState.SiteToken != "",
			"email":    appState.SiteEmail,
			"name":     appState.SiteName,
			"handle":   appState.SiteHandle,
			"deviceId": DeviceID()[:10],
		})
	})

	mux.HandleFunc("/api/site/logout", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		appState.SiteToken = ""
		appState.SiteEmail = ""
		appState.SiteName = ""
		appState.SiteHandle = ""
		saveStateLocked()
		mu.Unlock()
		writeJSON(w, map[string]any{"ok": true})
	})

	// Настройки GitHub-токена (снимает лимит 60 запросов/час)
	mux.HandleFunc("/api/github/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		appState.GithubToken = strings.TrimSpace(body.Token)
		saveStateLocked()
		mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "hasToken": strings.TrimSpace(body.Token) != ""})
	})
}

// ---------- установка ------------------------------------------------------

type importRequest struct {
	Slug      string
	Repo      string
	AssetURL  string
	AssetName string
	Version   string
	Name      string
	Category  string
	Tags      []string
	Note      string
	Launch    LaunchConfig
}

// isGithubDownloadURL оставлен для читаемости вызовов, логика — в
// isAllowedDownloadURL (releases.go): только https, github.com и поддомены
// githubusercontent.com. Суффикс проверяется по точке, чтобы
// "evilgithubusercontent.com" не проходил.
func isGithubDownloadURL(raw string) bool {
	return isAllowedDownloadURL(raw)
}

func slugFromRepo(repo string) string {
	s := strings.ToLower(strings.ReplaceAll(repo, "/", "-"))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	return out
}

func findCustom(slug string) (CustomApp, bool) {
	mu.Lock()
	defer mu.Unlock()
	app, ok := appState.Custom[slug]
	return app, ok
}

func installCustomApp(ctx context.Context, req importRequest) (CustomApp, error) {
	mu.Lock()
	root := appState.InstallRoot
	mu.Unlock()
	if root == "" {
		root = getDefaultInstallRoot()
	}
	destDir := filepath.Join(root, "custom", req.Slug)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return CustomApp{}, err
	}
	tmp, err := os.CreateTemp("", "artecraft-import-*"+filepath.Ext(req.AssetName))
	if err != nil {
		return CustomApp{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	sum, sizeMb, err := downloadToFile(ctx, req.Slug, req.AssetURL, tmpPath)
	if err != nil {
		return CustomApp{}, err
	}

	updateTask(req.Slug, func(t *TaskProgress) {
		t.Stage = "extract"
		t.Message = ""
		t.MessageKey = "task.extracting"
		t.Percent = 92
	})

	execPath := tmpPath
	kind := assetKind(req.AssetName)
	switch kind {
	case "zip":
		execPath, err = unzipArchive(tmpPath, destDir)
	case "targz":
		execPath, err = untarGz(tmpPath, destDir)
	case "deb":
		execPath, err = extractDeb(tmpPath, destDir)
	case "rpm":
		return CustomApp{}, uerr("err.rpmUnsupported", "")
	case "appimage", "exe", "msi", "dmg":
		target := filepath.Join(destDir, filepath.Base(req.AssetName))
		if err = copyFile(tmpPath, target, 0755); err == nil {
			execPath = target
		}
	default:
		target := filepath.Join(destDir, filepath.Base(req.AssetName))
		if err = copyFile(tmpPath, target, 0755); err == nil {
			execPath = target
		}
	}
	if err != nil {
		return CustomApp{}, err
	}
	if execPath == "" {
		return CustomApp{}, uerr("err.execNotFound", req.AssetName)
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(strings.ToLower(execPath), ".app") {
		_ = os.Chmod(execPath, 0755)
	}

	app := CustomApp{
		Slug:          req.Slug,
		Name:          firstNonEmpty(req.Name, req.Repo),
		Repo:          req.Repo,
		Version:       req.Version,
		AssetName:     req.AssetName,
		AssetURL:      req.AssetURL,
		ExecPath:      execPath,
		InstallFolder: destDir,
		SizeMb:        sizeMb,
		Sha256:        sum,
		InstalledAt:   time.Now().Format(time.RFC3339),
		Category:      firstNonEmpty(req.Category, "custom"),
		Tags:          req.Tags,
		Note:          req.Note,
		Untrusted:     true,
	}

	mu.Lock()
	if appState.Custom == nil {
		appState.Custom = map[string]CustomApp{}
	}
	appState.Custom[req.Slug] = app
	if req.Launch.Args != "" || req.Launch.Cwd != "" || req.Launch.PostInstall != "" || len(req.Launch.Profiles) > 0 {
		if appState.Launch == nil {
			appState.Launch = map[string]LaunchConfig{}
		}
		appState.Launch[req.Slug] = req.Launch
	}
	saveStateLocked()
	mu.Unlock()

	if strings.TrimSpace(req.Launch.PostInstall) != "" {
		go runHook(req.Launch.PostInstall, destDir, req.Slug)
	}
	return app, nil
}

func downloadToFile(ctx context.Context, slug, rawURL, dst string) (string, float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "ArteCraft-Launcher/"+LauncherVersion)
	if !isAllowedDownloadURL(rawURL) {
		return "", 0, uerr("err.hostBlocked", rawURL)
	}
	resp, err := githubOnlyClient(0).Do(req)
	if err != nil {
		return "", 0, errNet(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", 0, errHTTP(resp.StatusCode)
	}
	totalMb := float64(resp.ContentLength) / (1024 * 1024)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	written := int64(0)
	buf := make([]byte, 256*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", 0, werr
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			if time.Since(last) > 200*time.Millisecond {
				pct := 0.0
				if totalMb > 0 {
					pct = float64(written) / (1024 * 1024) / totalMb * 90
				}
				updateTask(slug, func(t *TaskProgress) {
					t.Stage = "download"
					t.Percent = pct
					t.DownloadedMB = float64(written) / (1024 * 1024)
					t.TotalMB = totalMb
					t.Message = ""
					t.MessageKey = "task.downloading"
				})
				last = time.Now()
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", 0, rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), float64(written) / (1024 * 1024), nil
}

func untarGz(src, destDir string) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var candidates []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		target := filepath.Join(destDir, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)|0755)
			if err != nil {
				continue
			}
			_, _ = io.Copy(out, io.LimitReader(tr, 1<<31))
			_ = out.Close()
			if isExecutableName(hdr.Name) {
				candidates = append(candidates, target)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) < len(candidates[j]) })
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", uerr("err.execNotFound", "")
}

func isExecutableName(name string) bool {
	low := strings.ToLower(name)
	switch runtime.GOOS {
	case "windows":
		return strings.HasSuffix(low, ".exe")
	case "darwin":
		return strings.HasSuffix(low, ".app") || strings.Contains(low, ".app/contents/macos/")
	default:
		base := filepath.Base(low)
		if strings.HasSuffix(base, ".appimage") || strings.HasSuffix(base, ".desktop") {
			return false
		}
		for _, bad := range []string{".txt", ".md", ".json", ".yml", ".yaml", ".png", ".svg", ".so", ".license", ".html", ".css", ".js"} {
			if strings.HasSuffix(base, bad) {
				return false
			}
		}
		return true
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	_ = os.MkdirAll(filepath.Dir(dst), 0755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	_ = out.Close()
	return cerr
}

func startWithConfig(execPath string, args []string, cwd string, env map[string]string) error {
	if runtime.GOOS == "darwin" && strings.HasSuffix(strings.ToLower(execPath), ".app") {
		cmd := exec.Command("open", append([]string{execPath, "--args"}, args...)...)
		return cmd.Start()
	}
	cmd := exec.Command(execPath, args...)
	if cwd == "" {
		cwd = filepath.Dir(execPath)
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.Start()
}

// runHook выполняет пользовательскую команду после установки (или по кнопке).
// Это осознанно «опасная» функция: она запускает то, что написал пользователь.
func runHook(command, dir, slug string) {
	if strings.TrimSpace(command) == "" {
		return
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if len(msg) > 4000 {
		msg = msg[len(msg)-4000:]
	}
	updateTask(slug, func(t *TaskProgress) {
		if err != nil {
			t.Message = "post-install: " + err.Error() + " " + msg
		} else {
			t.Message = "post-install: ok " + msg
		}
	})
}

func splitArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inQuote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------- CLI ------------------------------------------------------------

// RunCLIIfRequested вызывается до flag.Parse(), иначе -h и незнакомые флаги
// перехватывает стандартный flag package и печатает свою справку.
func RunCLIIfRequested(raw []string) bool {
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case "list", "install", "launch", "version", "help",
		"--list", "--install", "--launch", "--version", "--help", "-h":
		return RunCLI(raw)
	}
	return false
}

// RunCLI обрабатывает команды вида `artecraft --list`, `--install`, `--launch`.
// Возвращает true, если команда выполнена и UI запускать не нужно.
func RunCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	cmd := args[0]
	rest := args[1:]
	loadState()
	switch cmd {
	case "--list", "list":
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("%-22s %-10s %-12s %s\n", "SLUG", "VERSION", "SOURCE", "PATH")
		for _, app := range Catalog {
			inst, ok := appState.Installed[app.Slug]
			if !ok {
				continue
			}
			fmt.Printf("%-22s %-10s %-12s %s\n", app.Slug, inst.Version, "catalog", inst.ExecPath)
		}
		for slug, app := range appState.Custom {
			fmt.Printf("%-22s %-10s %-12s %s\n", slug, app.Version, "github", app.ExecPath)
		}
		return true
	case "--install", "install":
		if len(rest) == 0 {
			fmt.Println("usage: artecraft install <owner/repo> [asset-substring]")
			return true
		}
		target := rest[0]
		if strings.Contains(target, "/") && !strings.HasPrefix(target, "http") && len(strings.Split(target, "/")) == 2 {
			repo, err := NormalizeRepoURL(target)
			if err != nil {
				fmt.Println("error:", err)
				return true
			}
			app, err := cliInstallFromGithub(repo, pickArg(rest, 1))
			if err != nil {
				fmt.Println("error:", err)
				return true
			}
			fmt.Printf("installed %s %s -> %s\n", app.Name, app.Version, app.ExecPath)
			return true
		}
		// каталожное приложение
		for _, app := range Catalog {
			if app.Slug == target {
				fmt.Println("catalog installs use the UI/worker queue; run: artecraft --launch", app.Slug)
				return true
			}
		}
		fmt.Println("unknown target:", target)
		return true
	case "--launch", "launch":
		if len(rest) == 0 {
			fmt.Println("usage: artecraft launch <slug> [--args \"...\"]")
			return true
		}
		slug := rest[0]
		extra := pickFlag(rest, "--args")
		mu.Lock()
		cfg := appState.Launch[slug]
		custom, hasCustom := appState.Custom[slug]
		inst, hasInst := appState.Installed[slug]
		mu.Unlock()
		args := splitArgs(firstNonEmpty(extra, cfg.Args))
		cwd := cfg.Cwd
		if hasCustom {
			if err := startWithConfig(custom.ExecPath, args, cwd, cfg.Env); err != nil {
				fmt.Println("error:", err)
				return true
			}
			fmt.Println("launched", custom.ExecPath)
			return true
		}
		if hasInst {
			if err := startWithConfig(inst.ExecPath, args, cwd, cfg.Env); err != nil {
				fmt.Println("error:", err)
				return true
			}
			fmt.Println("launched", inst.ExecPath)
			return true
		}
		fmt.Println("not installed:", slug)
		return true
	case "--version", "version":
		fmt.Println("ArteCraft launcher", LauncherVersion)
		return true
	case "--help", "help", "-h":
		fmt.Println(`ArteCraft launcher

  artecraft                     открыть интерфейс
  artecraft list                установленные приложения
  artecraft install <repo> [ассет]   установить с GitHub (owner/repo)
  artecraft launch <slug> [--args "..."]
  artecraft --version`)
		return true
	}
	return false
}

func pickArg(args []string, i int) string {
	if len(args) > i {
		return args[i]
	}
	return ""
}

func pickFlag(args []string, name string) string {
	for i, a := range args {
		if a == name && len(args) > i+1 {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

func cliInstallFromGithub(repo, wantAsset string) (CustomApp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := githubRequest(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", &rel); err != nil {
		return CustomApp{}, err
	}
	var chosen *GithubAsset
	for _, a := range rel.Assets {
		if wantAsset != "" && strings.Contains(a.Name, wantAsset) {
			chosen = &GithubAsset{Name: a.Name, URL: a.URL, SizeMb: float64(a.Size) / (1024 * 1024)}
			break
		}
	}
	if chosen == nil {
		for _, a := range rel.Assets {
			if assetOS(a.Name) == currentOSKey() || assetOS(a.Name) == "any" {
				kind := assetKind(a.Name)
				if kind == "zip" || kind == "exe" || kind == "dmg" || kind == "appimage" || kind == "targz" {
					chosen = &GithubAsset{Name: a.Name, URL: a.URL, SizeMb: float64(a.Size) / (1024 * 1024)}
					break
				}
			}
		}
	}
	if chosen == nil {
		return CustomApp{}, uerr("err.noAsset", rel.TagName)
	}
	return installCustomApp(ctx, importRequest{
		Slug:      slugFromRepo(repo),
		Repo:      repo,
		AssetURL:  chosen.URL,
		AssetName: chosen.Name,
		Version:   strings.TrimPrefix(rel.TagName, "v"),
		Name:      repo,
		Category:  "custom",
	})
}

var _ = strconv.Itoa

// unzipArchive распаковывает zip и возвращает путь к первому исполняемому файлу.
func unzipArchive(src, destDir string) (string, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()
	var candidates []string
	for _, f := range zr.File {
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		target := filepath.Join(destDir, clean)
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode()|0755)
		if err != nil {
			_ = rc.Close()
			continue
		}
		_, _ = io.Copy(out, io.LimitReader(rc, 1<<31))
		_ = out.Close()
		_ = rc.Close()
		if isExecutableName(f.Name) {
			candidates = append(candidates, target)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) < len(candidates[j]) })
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", uerr("err.execNotFound", "")
}

// extractDeb распаковывает .deb в каталог приложения, не трогая систему:
// dpkg-deb -x, если он есть, иначе ручной разбор ar-архива + tar.
func extractDeb(debPath, destDir string) (string, error) {
	if p, err := exec.LookPath("dpkg-deb"); err == nil {
		if out, err := exec.Command(p, "-x", debPath, destDir).CombinedOutput(); err != nil {
			return "", fmt.Errorf("dpkg-deb: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return findExecInTree(destDir)
	}
	f, err := os.Open(debPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != "!<arch>\n" {
		return "", uerr("err.packageCorrupt", "deb")
	}
	for {
		hdr := make([]byte, 60)
		if _, err := io.ReadFull(f, hdr); err != nil {
			break
		}
		name := strings.TrimSpace(string(hdr[0:16]))
		sizeStr := strings.TrimSpace(string(hdr[48:58]))
		size, err := strconv.Atoi(sizeStr)
		if err != nil {
			break
		}
		if strings.HasPrefix(name, "data.tar") {
			tmp, err := os.CreateTemp("", "artecraft-deb-data-*")
			if err != nil {
				return "", err
			}
			tmpName := tmp.Name()
			_, _ = io.CopyN(tmp, f, int64(size))
			_ = tmp.Close()
			defer os.Remove(tmpName)
			switch {
			case strings.HasSuffix(name, ".gz"):
				return untarGz(tmpName, destDir)
			case strings.HasSuffix(name, ".xz"), strings.HasSuffix(name, ".zst"):
				flag := "-xJf"
				if strings.HasSuffix(name, ".zst") {
					flag = "--zstd"
				}
				cmd := exec.Command("tar", flag, tmpName, "-C", destDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					return "", fmt.Errorf("tar %s: %v (%s)", flag, err, strings.TrimSpace(string(out)))
				}
				return findExecInTree(destDir)
			case strings.HasSuffix(name, ".tar"):
				cmd := exec.Command("tar", "-xf", tmpName, "-C", destDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					return "", fmt.Errorf("tar: %v (%s)", err, strings.TrimSpace(string(out)))
				}
				return findExecInTree(destDir)
			}
			return "", uerr("err.packageCorrupt", name)
		}
		if _, err := f.Seek(int64(size)+(int64(size)%2), io.SeekCurrent); err != nil {
			break
		}
	}
	return "", uerr("err.packageCorrupt", "data.tar")
}

// findExecInTree ищет главный исполняемый файл в распакованном пакете:
// приоритет — usr/bin, opt/*/bin, затем всё остальное.
func findExecInTree(root string) (string, error) {
	var primary, secondary []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !isExecutableFile(p) {
			return nil
		}
		low := strings.ToLower(p)
		if strings.Contains(low, "/usr/bin/") || strings.Contains(low, "/opt/") && strings.Contains(low, "/bin/") {
			primary = append(primary, p)
		} else if strings.Contains(low, "/bin/") || strings.Contains(low, "/sbin/") {
			secondary = append(secondary, p)
		}
		return nil
	})
	pick := func(list []string) string {
		sort.Slice(list, func(i, j int) bool { return len(list[i]) < len(list[j]) })
		for _, p := range list {
			base := strings.ToLower(filepath.Base(p))
			if strings.Contains(base, "launcher") || strings.Contains(base, "uninstall") || strings.Contains(base, "-cli") {
				continue
			}
			return p
		}
		if len(list) > 0 {
			return list[0]
		}
		return ""
	}
	if p := pick(primary); p != "" {
		return p, nil
	}
	if p := pick(secondary); p != "" {
		return p, nil
	}
	return "", uerr("err.execNotFound", "")
}

func isExecutableFile(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.HasSuffix(strings.ToLower(p), ".exe")
	}
	return st.Mode()&0111 != 0 || strings.HasSuffix(strings.ToLower(p), ".appimage")
}
