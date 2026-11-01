package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// releases.go — как лаунчер узнаёт, какой файл качать для официальных записей.
//
// Было две беды:
//  1. Ассеты брались через api.github.com на каждый запуск — 13 запросов при
//     лимите 60 в час без токена. Лимит выбивался, и код падал на «запасную»
//     ссылку с устаревшей версией (0.16.0 против реальной 0.18.0) → 404.
//  2. Запасная ссылка вообще не проверялась, поэтому «базовые приложения не
//     качаются как надо».
//
// Стало: сначала кэш на диске (6 часов), потом редирект github.com/releases/latest
// (без API и без лимита) + HEAD по шаблонам имён ассетов, и только в конце —
// GitHub API, затем последний удачный кэш, и в самом конце — шаблон из версии по
// умолчанию. Любая скачанная ссылка проходит проверку хоста: только
// github.com и *.githubusercontent.com.

const releaseCacheTTL = 6 * time.Hour

type releaseCacheFile struct {
	Updated int64                        `json:"updated"`
	Items   map[string]RemoteReleaseInfo `json:"items"`
}

var releaseCacheMu sync.Mutex

func releaseCachePath() string {
	return filepath.Join(getAppDataDir(), "releases-cache.json")
}

func loadReleaseCache() map[string]RemoteReleaseInfo {
	releaseCacheMu.Lock()
	defer releaseCacheMu.Unlock()
	out := map[string]RemoteReleaseInfo{}
	raw, err := os.ReadFile(releaseCachePath())
	if err != nil {
		return out
	}
	var f releaseCacheFile
	if json.Unmarshal(raw, &f) != nil {
		return out
	}
	for k, v := range f.Items {
		out[k] = v
	}
	return out
}

func saveReleaseCache(items map[string]RemoteReleaseInfo) {
	releaseCacheMu.Lock()
	defer releaseCacheMu.Unlock()
	_ = os.MkdirAll(getAppDataDir(), 0755)
	raw, err := json.MarshalIndent(releaseCacheFile{Updated: time.Now().Unix(), Items: items}, "", "  ")
	if err != nil {
		return
	}
	tmp := releaseCachePath() + ".tmp"
	if os.WriteFile(tmp, raw, 0644) == nil {
		_ = os.Rename(tmp, releaseCachePath())
	}
}

func releaseCacheAge() time.Duration {
	raw, err := os.ReadFile(releaseCachePath())
	if err != nil {
		return time.Duration(1<<62 - 1)
	}
	var f releaseCacheFile
	if json.Unmarshal(raw, &f) != nil || f.Updated == 0 {
		return time.Duration(1<<62 - 1)
	}
	return time.Since(time.Unix(f.Updated, 0))
}

// githubOnlyClient — HTTP-клиент, который не даёт уйти за пределы GitHub.
// Проверяется каждый шаг редиректа, а не только стартовый URL: именно так
// ловятся «зеркала» и подменённые ссылки в чужих релизах.
func githubOnlyClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return uerr("err.tooManyRedirects", "")
			}
			if !isAllowedDownloadURL(req.URL.String()) {
				return uerr("err.hostBlocked", req.URL.Host)
			}
			return nil
		},
	}
}

// allowLocalMirror — только для тестов: ARTECRAFT_ALLOW_LOCAL_MIRROR=1
// разрешает фикстуру на 127.0.0.1. В обычном запуске переменной нет, и правило
// «скачиваем только с GitHub» остаётся жёстким.
func allowLocalMirror() bool {
	return os.Getenv("ARTECRAFT_ALLOW_LOCAL_MIRROR") == "1"
}

// isAllowedDownloadURL — единая точка правды о разрешённых хостах.
func isAllowedDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if allowLocalMirror() && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return u.Scheme == "https" || u.Scheme == "http"
	}
	if u.Scheme != "https" {
		return false
	}
	switch {
	case host == "github.com":
		return true
	case host == "githubusercontent.com":
		return true
	case strings.HasSuffix(host, ".githubusercontent.com"):
		return true
	case strings.HasPrefix(host, "api.github."):
		return true
	}
	return false
}

// resolveLatestTag узнаёт тег последнего релиза по редиректу обычной страницы.
// Ответ API не нужен, поэтому лимит 60 запросов в час здесь не мешает.
func resolveLatestTag(repo string) string {
	req, err := http.NewRequest(http.MethodGet, "https://github.com/"+repo+"/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
	cl := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return ""
	}
	idx := strings.Index(loc, "/releases/tag/")
	if idx < 0 {
		return ""
	}
	tag := strings.TrimSuffix(loc[idx+len("/releases/tag/"):], "/")
	if q := strings.IndexByte(tag, '?'); q >= 0 {
		tag = tag[:q]
	}
	if tag == "" {
		return ""
	}
	if u, err := url.PathUnescape(tag); err == nil {
		tag = u
	}
	return tag
}

// assetNameCandidates — шаблоны имён файлов официальных сборок.
// Порядок важен: сначала портативные варианты, потом установщики.
func assetNameCandidates(slug, ver string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			fmt.Sprintf("%s-%s-macos-universal.dmg", slug, ver),
			fmt.Sprintf("%s-%s-macos-aarch64.dmg", slug, ver),
			fmt.Sprintf("%s-%s-macos-x86_64.dmg", slug, ver),
		}
	case "linux":
		arch := "x86_64"
		if runtime.GOARCH == "arm64" {
			arch = "aarch64"
		}
		return []string{
			fmt.Sprintf("%s-%s-linux-%s.tar.gz", slug, ver, arch),
			fmt.Sprintf("%s-%s-linux-%s.AppImage", slug, ver, arch),
		}
	default:
		arch := "x64"
		if runtime.GOARCH == "arm64" {
			arch = "arm64"
		}
		return []string{
			fmt.Sprintf("%s-%s-windows-%s-portable.zip", slug, ver, arch),
			fmt.Sprintf("%s-%s-windows-%s.msi", slug, ver, arch),
		}
	}
}

// headExists проверяет, что файл релиза действительно есть (GitHub отвечает 302
// на objects.githubusercontent.com — тело не скачиваем).
func headExists(rawURL string) bool {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
	cl := &http.Client{
		Timeout: 25 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !isAllowedDownloadURL(req.URL.String()) {
				return uerr("err.hostBlocked", req.URL.Host)
			}
			return nil
		},
	}
	resp, err := cl.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// resolveReleaseLive — сетевой путь: тег по редиректу, затем HEAD по шаблонам,
// затем (если шаблоны не совпали) GitHub API с реальными именами ассетов.
func resolveReleaseLive(meta AppMeta) (RemoteReleaseInfo, bool) {
	repo := repoPathOf(meta)
	if repo == "" {
		return RemoteReleaseInfo{}, false
	}
	tag := resolveLatestTag(repo)
	// 1) Имена файлов из HTML-страницы релиза: без GitHub API и без лимита 60/час.
	if rel, ok := resolveAssetFromPage(repo, tag); ok {
		return rel, true
	}
	ver := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if ver != "" {
		for _, name := range assetNameCandidates(meta.Slug, ver) {
			u := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
			if headExists(u) {
				return RemoteReleaseInfo{Version: ver, AssetName: name, DownloadURL: u}, true
			}
		}
	}
	// API — запасной путь: даёт точные имена (иногда отличается суффикс).
	if rel, ok := resolveViaAPI(meta, repo); ok {
		return rel, true
	}
	return RemoteReleaseInfo{}, false
}

func resolveViaAPI(meta AppMeta, repo string) (RemoteReleaseInfo, bool) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return RemoteReleaseInfo{}, false
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
	mu.Lock()
	token := appState.GithubToken
	mu.Unlock()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return RemoteReleaseInfo{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return RemoteReleaseInfo{}, false
	}
	var parsed struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
		return RemoteReleaseInfo{}, false
	}
	name, dl, size := pickAssetForCurrentOS(parsed.Assets)
	if dl == "" || !isAllowedDownloadURL(dl) {
		return RemoteReleaseInfo{}, false
	}
	return RemoteReleaseInfo{
		Version:     strings.TrimPrefix(parsed.TagName, "v"),
		AssetName:   name,
		DownloadURL: dl,
		SizeMB:      size,
	}, true
}

// repoPathOf достаёт owner/name из RepoURL официальной записи.
func repoPathOf(meta AppMeta) string {
	if meta.RepoURL == "" {
		return ""
	}
	repo, err := NormalizeRepoURL(meta.RepoURL)
	if err != nil {
		return ""
	}
	return repo
}

// resolveOneRelease — полный порядок: кэш → сеть → старый кэш → шаблон по умолчанию.
func resolveOneRelease(meta AppMeta, cached map[string]RemoteReleaseInfo, allowNetwork bool) (RemoteReleaseInfo, bool) {
	if rel, ok := cached[meta.Slug]; ok && rel.DownloadURL != "" {
		if allowNetwork {
			// кэш свежее TTL — сразу отдаём, иначе пробуем освежить
			if releaseCacheAge() < releaseCacheTTL {
				return rel, true
			}
		} else {
			return rel, true
		}
	}
	if allowNetwork {
		if rel, ok := resolveReleaseLive(meta); ok {
			return rel, true
		}
	}
	if rel, ok := cached[meta.Slug]; ok && rel.DownloadURL != "" {
		return rel, true
	}
	return fallbackReleaseForOS(meta), true
}

// refreshReleases обновляет карту релизов и кэш на диске. Вызывается на старте
// (в фоне) и из /api/refresh-releases.
func refreshReleases() {
	cached := loadReleaseCache()
	next := map[string]RemoteReleaseInfo{}
	mu.Lock()
	current := map[string]RemoteReleaseInfo{}
	for k, v := range releasesMap {
		current[k] = v
	}
	mu.Unlock()

	changed := false
	for _, app := range Catalog {
		rel, ok := resolveOneRelease(app, cached, true)
		if !ok {
			continue
		}
		if prev, had := current[app.Slug]; !had || prev.DownloadURL != rel.DownloadURL {
			changed = true
		}
		next[app.Slug] = rel
	}
	if len(next) == 0 {
		return
	}
	mu.Lock()
	for k, v := range next {
		releasesMap[k] = v
	}
	mu.Unlock()
	saveReleaseCache(next)
	_ = changed
}

// resolveReleaseForInstall — вызывается воркером прямо перед скачиванием:
// если известная ссылка «запасная» (не подтверждена сетью), пробуем уточнить.
func resolveReleaseForInstall(meta AppMeta) RemoteReleaseInfo {
	mu.Lock()
	rel, had := releasesMap[meta.Slug]
	mu.Unlock()
	if had && rel.DownloadURL != "" && isAllowedDownloadURL(rel.DownloadURL) && headExists(rel.DownloadURL) {
		return rel
	}
	fresh, ok := resolveOneRelease(meta, loadReleaseCache(), true)
	if ok && fresh.DownloadURL != "" {
		mu.Lock()
		releasesMap[meta.Slug] = fresh
		mu.Unlock()
		return fresh
	}
	if had {
		return rel
	}
	return fallbackReleaseForOS(meta)
}

// ---------------------------------------------------------------------------
// Ассеты без GitHub API
//
// Раньше имена файлов релиза узнавались только через api.github.com, а он даёт
// 60 запросов в час на IP. Пользователь ставил пару приложений, лимит кончался,
// preview возвращал пустой downloadUrl — и установка падала с невнятным
// «Only GitHub links are supported». Теперь список файлов берётся из обычной
// HTML-страницы релиза: https://github.com/{repo}/releases/expanded_assets/{tag}
// Она отдаётся без авторизации и без лимита, а ссылки в ней уже готовые.
// ---------------------------------------------------------------------------

var assetHrefRe = regexp.MustCompile(`href="(/[^"/]+/[^"/]+/releases/download/[^"]+)"`)

// assetsFromExpandedPage возвращает файлы релиза, разобранные из HTML GitHub.
func assetsFromExpandedPage(repo, tag string) []GithubAsset {
	if repo == "" || tag == "" {
		return nil
	}
	u := fmt.Sprintf("https://github.com/%s/releases/expanded_assets/%s", repo, url.PathEscape(tag))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
	req.Header.Set("Accept", "text/html")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]GithubAsset, 0, 8)
	for _, m := range assetHrefRe.FindAllStringSubmatch(string(body), -1) {
		href := m[1]
		name := href
		if i := strings.LastIndex(href, "/"); i >= 0 {
			name = href[i+1:]
		}
		if dec, derr := url.PathUnescape(name); derr == nil {
			name = dec
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		low := strings.ToLower(name)
		service := false
		for _, bad := range []string{"dsym", "sources", "debug", "dbgsym", "ddeb", "checksum", "sha256", ".sig", ".asc", "symbols"} {
			if strings.Contains(low, bad) {
				service = true
			}
		}
		out = append(out, GithubAsset{
			Name:    name,
			URL:     "https://github.com" + href,
			OS:      assetOS(name),
			Kind:    assetKind(name),
			Service: service,
		})
	}
	return out
}

// pickBestAsset выбирает файл под текущую ОС и архитектуру: сначала портативные
// архивы и установщики своей платформы, потом всё остальное; служебные файлы
// (символы, контрольные суммы) и чужие архитектуры штрафуются.
func pickBestAsset(assets []GithubAsset) int {
	best, bestScore := -1, -1
	for i, a := range assets {
		if a.Service {
			continue
		}
		score := 0
		if a.OS == currentOSKey() {
			score += 10
		} else if a.OS == "any" {
			score += 2
		} else {
			continue
		}
		// Портативные сборки в приоритете: лаунчер их просто распаковывает и
		// запускает. Системные пакеты (deb/rpm) ставятся в обход менеджера
		// пакетов, поэтому берём их только если ничего портативного нет.
		switch a.Kind {
		case "appimage", "dmg", "targz":
			score += 8
		case "zip":
			score += 7
		case "msi", "exe":
			score += 4
		case "deb", "rpm", "archpkg":
			score += 1
		}
		low := strings.ToLower(a.Name)
		for _, bad := range []string{"-cli-", "-server-", "debug", "symbols", "sha256", ".sig", ".asc", "checksum", "dsym", "ddeb", "sources", "dbgsym", ".src."} {
			if strings.Contains(low, bad) {
				score -= 20
			}
		}
		// Архитектура: раньше штрафовались только «arm64» и «x86_64», поэтому на
		// обычном ПК спокойно выбирался Godot_..._linux.arm32.zip. Теперь чужая
		// разрядность отсекается жёстко.
		arm64 := runtime.GOARCH == "arm64"
		isArm64 := strings.Contains(low, "arm64") || strings.Contains(low, "aarch64")
		isArm32 := strings.Contains(low, "arm32") || strings.Contains(low, "armhf") || strings.Contains(low, "armel") ||
			strings.Contains(low, "armv7") || strings.Contains(low, "armv6")
		isX64 := strings.Contains(low, "x86_64") || strings.Contains(low, "amd64") || strings.Contains(low, "x64")
		isX86 := strings.Contains(low, "i386") || strings.Contains(low, "i686") || strings.Contains(low, "x86_32") ||
			strings.Contains(low, "win32") || strings.Contains(low, "32-bit") || strings.Contains(low, "386")
		if isArm64 {
			if arm64 {
				score += 6
			} else {
				score -= 25
			}
		}
		if isArm32 {
			score -= 25
		}
		if isX64 && !arm64 {
			score += 6
		}
		if isX86 {
			score -= 25
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	return best
}

// resolveAssetFromPage — лучший файл последнего релиза без обращения к API.
func resolveAssetFromPage(repo, tag string) (RemoteReleaseInfo, bool) {
	assets := assetsFromExpandedPage(repo, tag)
	if len(assets) == 0 {
		return RemoteReleaseInfo{}, false
	}
	if i := pickBestAsset(assets); i >= 0 && isAllowedDownloadURL(assets[i].URL) {
		return RemoteReleaseInfo{
			Version:     strings.TrimPrefix(strings.TrimSpace(tag), "v"),
			AssetName:   assets[i].Name,
			DownloadURL: assets[i].URL,
			SizeMB:      assets[i].SizeMb,
		}, true
	}
	return RemoteReleaseInfo{}, false
}

// resolveAssetForRepo — «дай мне подходящий файл релиза для owner/name».
// Порядок: страница expanded_assets (без лимита) → шаблоны имён → API.
func resolveAssetForRepo(repo string) (RemoteReleaseInfo, bool) {
	tag := resolveLatestTag(repo)
	if tag != "" {
		if rel, ok := resolveAssetFromPage(repo, tag); ok {
			return rel, true
		}
		ver := strings.TrimPrefix(strings.TrimSpace(tag), "v")
		slug := slugFromRepo(repo)
		if ver != "" {
			for _, name := range assetNameCandidates(slug, ver) {
				u := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
				if headExists(u) {
					return RemoteReleaseInfo{Version: ver, AssetName: name, DownloadURL: u}, true
				}
			}
		}
	}
	if rel, ok := resolveViaAPI(AppMeta{RepoURL: repo, Slug: slugFromRepo(repo)}, repo); ok {
		return rel, true
	}
	return RemoteReleaseInfo{}, false
}
