package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func init() {
	// CRITICAL for macOS Cocoa/AppKit & Linux GTK: main() MUST remain pinned
	// to OS Thread 0 (pthread_main_np() == 1) across goroutine spawns.
	runtime.LockOSThread()
}

//go:embed ui/*
var embeddedUI embed.FS

const LauncherVersion = "2.5.0"

type AppMeta struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Replaces       string   `json:"replaces"`
	Category       string   `json:"category"`
	Accent         string   `json:"accent"`
	Stars          string   `json:"stars"`
	SavesUsd       int      `json:"savesUsd"`
	DefaultVersion string   `json:"defaultVersion"`
	Formats        []string `json:"formats"`
	RepoURL        string   `json:"repoUrl"`
}

type InstalledApp struct {
	Slug          string  `json:"slug"`
	Version       string  `json:"version"`
	InstallDir    string  `json:"installDir"`
	InstallFolder string  `json:"installFolder"`
	ExecPath      string  `json:"execPath"`
	SizeMB        float64 `json:"sizeMb,omitempty"`
	InstalledAt   string  `json:"installedAt"`
}

type RemoteReleaseInfo struct {
	Version     string  `json:"version"`
	DownloadURL string  `json:"downloadUrl"`
	AssetName   string  `json:"assetName"`
	SizeMB      float64 `json:"sizeMb"`
}

type TaskProgress struct {
	Slug         string  `json:"slug"`
	Stage        string  `json:"stage"` // queued, downloading, extracting, done, error, canceled
	Percent      float64 `json:"percent"`
	DownloadedMB float64 `json:"downloadedMb"`
	TotalMB      float64 `json:"totalMb"`
	SpeedMBps    float64 `json:"speedMbps"`
	Sha256       string  `json:"sha256,omitempty"`
	Error        string  `json:"error,omitempty"`
	Message      string  `json:"message,omitempty"`
	MessageKey   string  `json:"messageKey,omitempty"`
	ErrorKey     string  `json:"errorKey,omitempty"`
	ErrorDetail  string  `json:"errorDetail,omitempty"`
	UpdatedAt    int64   `json:"updatedAt"`
}

type SelfUpdateStatus struct {
	Available   bool              `json:"available"`
	Locked      bool              `json:"locked"`
	ShowBanner  bool              `json:"showBanner"`
	RemoteVer   string            `json:"remoteVer"`
	DownloadURL string            `json:"downloadUrl"`
	Title       map[string]string `json:"title,omitempty"`
	Message     map[string]string `json:"message,omitempty"`
}

type PersistedState struct {
	ClientID        string                  `json:"clientId"`
	Locale          string                  `json:"locale"`
	InstallRoot     string                  `json:"installRoot"`
	AutoStart       bool                    `json:"autoStart"`
	CloseToTray     bool                    `json:"closeToTray"`
	CreateShortcuts bool                    `json:"createShortcuts"`
	CreateStartMenu bool                    `json:"createStartMenu"`
	LowSpecMode     bool                    `json:"lowSpecMode"`
	Installed       map[string]InstalledApp `json:"installed"`

	// --- маркетплейс и импорт с GitHub (market.go) ---
	Custom      map[string]CustomApp    `json:"custom,omitempty"`
	Launch      map[string]LaunchConfig `json:"launch,omitempty"`
	SiteToken   string                  `json:"siteToken,omitempty"`
	SiteEmail   string                  `json:"siteEmail,omitempty"`
	SiteName    string                  `json:"siteName,omitempty"`
	SiteHandle  string                  `json:"siteHandle,omitempty"`
	GithubToken string                  `json:"githubToken,omitempty"`
}

// InstallOptions — параметры установки, которые пользователь выбирает в диалоге.
// Нил-поля означают «не задано»: тогда берутся значения из настроек лаунчера.
type InstallOptions struct {
	Dir         string `json:"dir,omitempty"`
	Shortcut    *bool  `json:"shortcut,omitempty"`
	StartMenu   *bool  `json:"startMenu,omitempty"`
	LaunchAfter *bool  `json:"launchAfter,omitempty"`
	Args        string `json:"args,omitempty"`
	Verify      *bool  `json:"verify,omitempty"`
}

var installOptsMap = map[string]InstallOptions{}

var Catalog = []AppMeta{
	{Slug: "photocraft", Name: "PhotoCraft", Replaces: "Adobe Photoshop", Category: "creative", Accent: "#3b82f6", Stars: "70.9k", SavesUsd: 275, DefaultVersion: "0.18.0", Formats: []string{".psd", ".psb", ".abr", ".raw", ".webp", ".png"}, RepoURL: "https://github.com/storytold/photocraft"},
	{Slug: "wordcraft", Name: "WordCraft", Replaces: "Microsoft Word", Category: "office", Accent: "#2563eb", Stars: "15.4k", SavesUsd: 99, DefaultVersion: "0.13.2", Formats: []string{".docx", ".doc", ".odt", ".rtf", ".pdf", ".md"}, RepoURL: "https://github.com/storytold/wordcraft"},
	{Slug: "pdfcraft", Name: "PDFCraft", Replaces: "Adobe Acrobat Pro", Category: "office", Accent: "#ef4444", Stars: "17.1k", SavesUsd: 239, DefaultVersion: "0.16.1", Formats: []string{".pdf", ".fdf", ".xfdf", ".pdfa"}, RepoURL: "https://github.com/storytold/pdfcraft"},
	{Slug: "gridcraft", Name: "GridCraft", Replaces: "Microsoft Excel", Category: "office", Accent: "#10b981", Stars: "10.6k", SavesUsd: 99, DefaultVersion: "0.14.2", Formats: []string{".xlsx", ".xls", ".csv", ".ods", ".parquet"}, RepoURL: "https://github.com/storytold/gridcraft"},
	{Slug: "deckcraft", Name: "DeckCraft", Replaces: "Microsoft PowerPoint", Category: "office", Accent: "#f97316", Stars: "7.9k", SavesUsd: 99, DefaultVersion: "0.13.1", Formats: []string{".pptx", ".ppt", ".odp", ".pdf"}, RepoURL: "https://github.com/storytold/deckcraft"},
	{Slug: "cadcraft", Name: "CADCraft", Replaces: "AutoCAD", Category: "engineering-ui", Accent: "#eab308", Stars: "12.1k", SavesUsd: 2030, DefaultVersion: "0.15.1", Formats: []string{".dwg", ".dxf", ".step", ".iges", ".stl"}, RepoURL: "https://github.com/storytold/cadcraft"},
	{Slug: "figcraft", Name: "FigCraft", Replaces: "Figma & Visio", Category: "engineering-ui", Accent: "#a855f7", Stars: "13.5k", SavesUsd: 180, DefaultVersion: "0.13.1", Formats: []string{".fig", ".vsdx", ".svg", ".pdf"}, RepoURL: "https://github.com/storytold/figcraft"},
	{Slug: "filmcraft", Name: "FilmCraft", Replaces: "Adobe Premiere Pro", Category: "video-audio", Accent: "#8b5cf6", Stars: "15.6k", SavesUsd: 275, DefaultVersion: "0.15.0", Formats: []string{".mp4", ".mov", ".mkv", ".prores", ".fcpxml"}, RepoURL: "https://github.com/storytold/filmcraft"},
	{Slug: "soundcraft", Name: "SoundCraft", Replaces: "Avid Pro Tools / Audition", Category: "video-audio", Accent: "#06b6d4", Stars: "8.4k", SavesUsd: 299, DefaultVersion: "0.15.0", Formats: []string{".wav", ".flac", ".mp3", ".ogg", ".opus"}, RepoURL: "https://github.com/storytold/soundcraft"},
	{Slug: "lightcraft", Name: "LightCraft", Replaces: "Adobe Lightroom Classic", Category: "creative", Accent: "#0ea5e9", Stars: "14.2k", SavesUsd: 119, DefaultVersion: "0.16.0", Formats: []string{".cr3", ".nef", ".arw", ".dng", ".raf"}, RepoURL: "https://github.com/storytold/lightcraft"},
	{Slug: "vectorcraft", Name: "VectorCraft", Replaces: "Adobe Illustrator", Category: "creative", Accent: "#f59e0b", Stars: "10.8k", SavesUsd: 275, DefaultVersion: "0.19.0", Formats: []string{".ai", ".svg", ".eps", ".pdf"}, RepoURL: "https://github.com/storytold/vectorcraft"},
	{Slug: "effectcraft", Name: "EffectCraft", Replaces: "Adobe After Effects", Category: "video-audio", Accent: "#6366f1", Stars: "9.5k", SavesUsd: 275, DefaultVersion: "0.19.0", Formats: []string{".aep", ".lottie", ".mp4", ".mov", ".exr"}, RepoURL: "https://github.com/storytold/effectcraft"},
	{Slug: "designcraft", Name: "DesignCraft", Replaces: "Adobe InDesign", Category: "creative", Accent: "#ec4899", Stars: "7.6k", SavesUsd: 275, DefaultVersion: "0.15.2", Formats: []string{".idml", ".indd", ".pdf", ".epub"}, RepoURL: "https://github.com/storytold/designcraft"},
}

var (
	mu             sync.Mutex
	appState       PersistedState
	releasesMap    = map[string]RemoteReleaseInfo{}
	tasksMap       = map[string]*TaskProgress{}
	cancelFuncs    = map[string]context.CancelFunc{}
	selfUpd        = SelfUpdateStatus{DownloadURL: "https://getartecraft.com/download"}
	installCh      = make(chan string, 32)
	lastPollUnixMs atomic.Int64
	hasSeenClient  atomic.Bool
)

func candidateOrigins() []string {
	if custom := strings.TrimSpace(os.Getenv("ARTECRAFT_ORIGIN")); custom != "" {
		return []string{strings.TrimRight(custom, "/")}
	}
	return []string{"https://getartecraft.com"}
}

func osDisplayLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return "Windows"
	}
}

func archDisplayLabel() string {
	if runtime.GOARCH == "amd64" {
		return "x64"
	}
	return runtime.GOARCH
}

func getAppDataDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ArteCraft")
	case "linux":
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "ArteCraft")
		}
		return filepath.Join(home, ".config", "ArteCraft")
	default:
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, "ArteCraft")
		}
		return filepath.Join(home, "ArteCraft")
	}
}

func getDefaultInstallRoot() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Applications", "ArteCraft")
	case "linux":
		return filepath.Join(home, ".local", "opt", "artecraft")
	default:
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, "Programs", "ArteCraft")
		}
		return filepath.Join(home, "ArteCraftApps")
	}
}

func stateFilePath() string {
	return filepath.Join(getAppDataDir(), "launcher-state-v2.json")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func loadState() {
	_ = os.MkdirAll(getAppDataDir(), 0755)
	appState = PersistedState{
		ClientID:        randomHex(12),
		Locale:          "",
		InstallRoot: getDefaultInstallRoot(),
		// Трей и автозапуск включены по умолчанию: так просил владелец — человек
		// ставит лаунчер, закрывает крестиком и ожидает, что тот останется в трее,
		// а после перезагрузки поднимется сам. Кому не надо — выключает в настройках.
		// json.Unmarshal не трогает отсутствующие ключи, поэтому явное false
		// из state.json уважается и обратно не перебивается.
		AutoStart:       true,
		CloseToTray:     true,
		CreateShortcuts: true,
		CreateStartMenu: true,
		LowSpecMode:     false,
		Installed:       map[string]InstalledApp{},
	}
	raw, err := os.ReadFile(stateFilePath())
	if err == nil {
		_ = json.Unmarshal(raw, &appState)
	}
	if appState.ClientID == "" {
		appState.ClientID = randomHex(12)
	}
	if appState.InstallRoot == "" {
		appState.InstallRoot = getDefaultInstallRoot()
	}
	if appState.Installed == nil {
		appState.Installed = map[string]InstalledApp{}
	}
	if appState.Custom == nil {
		appState.Custom = map[string]CustomApp{}
	}
	if appState.Launch == nil {
		appState.Launch = map[string]LaunchConfig{}
	}
	// чистим импортированные записи, у которых пропал файл
	for slug, app := range appState.Custom {
		if app.ExecPath == "" {
			delete(appState.Custom, slug)
			continue
		}
		if _, serr := os.Stat(app.ExecPath); serr != nil {
			delete(appState.Custom, slug)
		}
	}
	// Prune stale entries whose binaries were manually deleted on disk
	for slug, inst := range appState.Installed {
		if inst.InstallDir == "" && inst.InstallFolder != "" {
			inst.InstallDir = inst.InstallFolder
		}
		if inst.InstallFolder == "" && inst.InstallDir != "" {
			inst.InstallFolder = inst.InstallDir
		}
		if inst.ExecPath == "" {
			delete(appState.Installed, slug)
			continue
		}
		if _, serr := os.Stat(inst.ExecPath); serr != nil {
			delete(appState.Installed, slug)
			continue
		}
		appState.Installed[slug] = inst
	}
	_ = os.MkdirAll(appState.InstallRoot, 0755)
	saveStateLocked()
}

func saveStateLocked() {
	raw, err := json.MarshalIndent(appState, "", "  ")
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(stateFilePath()), 0755)
		_ = os.WriteFile(stateFilePath(), raw, 0644)
	}
}

func fallbackReleaseForOS(app AppMeta) RemoteReleaseInfo {
	ver := app.DefaultVersion
	repo := repoPathOf(app)
	if repo == "" {
		repo = "storytold/" + app.Slug
	}
	base := fmt.Sprintf("https://github.com/%s/releases/download/v%s", repo, ver)
	switch runtime.GOOS {
	case "darwin":
		asset := fmt.Sprintf("%s-%s-macos-universal.dmg", app.Slug, ver)
		return RemoteReleaseInfo{Version: ver, AssetName: asset, DownloadURL: base + "/" + asset, SizeMB: 22.0}
	case "linux":
		archPart := "x86_64"
		if runtime.GOARCH == "arm64" {
			archPart = "aarch64"
		}
		asset := fmt.Sprintf("%s-%s-linux-%s.tar.gz", app.Slug, ver, archPart)
		return RemoteReleaseInfo{Version: ver, AssetName: asset, DownloadURL: base + "/" + asset, SizeMB: 24.0}
	default:
		archPart := "x64"
		if runtime.GOARCH == "arm64" {
			archPart = "arm64"
		}
		asset := fmt.Sprintf("%s-%s-windows-%s-portable.zip", app.Slug, ver, archPart)
		return RemoteReleaseInfo{Version: ver, AssetName: asset, DownloadURL: base + "/" + asset, SizeMB: 19.0}
	}
}

func pickAssetForCurrentOS(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}) (string, string, float64) {
	switch runtime.GOOS {
	case "darwin":
		for _, a := range assets {
			low := strings.ToLower(a.Name)
			if strings.Contains(low, "-cli-") || strings.Contains(low, "-server-") {
				continue
			}
			if strings.HasSuffix(low, ".dmg") && (strings.Contains(low, "universal") || strings.Contains(low, "macos") || strings.Contains(low, "darwin")) {
				return a.Name, a.BrowserDownloadURL, float64(a.Size) / (1024 * 1024)
			}
		}
	case "linux":
		wantArch := "x86_64"
		if runtime.GOARCH == "arm64" {
			wantArch = "aarch64"
		}
		for _, a := range assets {
			low := strings.ToLower(a.Name)
			if strings.Contains(low, "-cli-") || strings.Contains(low, "-server-") || strings.HasSuffix(low, ".zsync") {
				continue
			}
			if strings.Contains(low, "linux") && strings.Contains(low, wantArch) && strings.HasSuffix(low, ".tar.gz") {
				return a.Name, a.BrowserDownloadURL, float64(a.Size) / (1024 * 1024)
			}
		}
		for _, a := range assets {
			low := strings.ToLower(a.Name)
			if strings.Contains(low, "-cli-") || strings.Contains(low, "-server-") || strings.HasSuffix(low, ".zsync") {
				continue
			}
			if strings.Contains(low, "linux") && strings.Contains(low, wantArch) && strings.HasSuffix(low, ".appimage") {
				return a.Name, a.BrowserDownloadURL, float64(a.Size) / (1024 * 1024)
			}
		}
	default:
		wantArch := "x64"
		if runtime.GOARCH == "arm64" {
			wantArch = "arm64"
		}
		for _, a := range assets {
			low := strings.ToLower(a.Name)
			if strings.Contains(low, "windows") && strings.Contains(low, wantArch) && strings.HasSuffix(low, "portable.zip") {
				return a.Name, a.BrowserDownloadURL, float64(a.Size) / (1024 * 1024)
			}
		}
	}
	return "", "", 0
}

// resolveAllReleases оставлен как точка входа для существующих вызовов,
// внутри — новый резолвер из releases.go (кэш на диске, без лимита GitHub API).
func resolveAllReleases() {
	refreshReleases()
}

func compareSemver(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	pb := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")
	for i := 0; i < 3; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			vb, _ = strconv.Atoi(pb[i])
		}
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}

// launcherLockedForUpdates — «блокировка фулл»: когда на сайте выставлен
// minVersion/forceBlockAll и текущая сборка ниже, установка приложений
// запрещается до обновления лаунчера. Проверка живёт на сервере, а не только в UI,
// иначе её обходил любой запрос к /api/install.
func launcherLockedForUpdates() (bool, SelfUpdateStatus) {
	mu.Lock()
	st := selfUpd
	mu.Unlock()
	if !st.Locked {
		return false, st
	}
	return true, st
}

func checkRemoteLauncherUpdate() SelfUpdateStatus {
	client := &http.Client{Timeout: 8 * time.Second}
	mu.Lock()
	cid := appState.ClientID
	mu.Unlock()
	for _, origin := range candidateOrigins() {
		u := fmt.Sprintf("%s/api/launcher/check?cid=%s&v=%s&version=%s&os=%s&arch=%s",
			origin, cid, LauncherVersion, LauncherVersion, runtime.GOOS, runtime.GOARCH)
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var payload struct {
			LatestVersion string            `json:"latestVersion"`
			MinVersion    string            `json:"minVersion"`
			ForceUpdate   bool              `json:"forceUpdate"`
			ForceBlockAll bool              `json:"forceBlockAll"`
			NotifyBanner  bool              `json:"notifyBanner"`
			DownloadURL   string            `json:"downloadUrl"`
			Title         map[string]string `json:"title"`
			Message       map[string]string `json:"message"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		_ = resp.Body.Close()
		if err != nil || payload.LatestVersion == "" {
			continue
		}
		newer := compareSemver(LauncherVersion, payload.LatestVersion) < 0
		belowMin := payload.MinVersion != "" && compareSemver(LauncherVersion, payload.MinVersion) < 0
		locked := payload.ForceBlockAll || belowMin || (newer && payload.ForceUpdate)
		dl := payload.DownloadURL
		if dl == "" {
			dl = "https://getartecraft.com/download"
		}
		st := SelfUpdateStatus{
			Available:   newer || locked,
			Locked:      locked,
			ShowBanner:  !locked && newer && payload.NotifyBanner,
			RemoteVer:   payload.LatestVersion,
			DownloadURL: dl,
			Title:       payload.Title,
			Message:     payload.Message,
		}
		mu.Lock()
		selfUpd = st
		mu.Unlock()
		return st
	}
	mu.Lock()
	defer mu.Unlock()
	return selfUpd
}

func sendTelemetry(event, appSlug string) {
	go func() {
		mu.Lock()
		cid := appState.ClientID
		mu.Unlock()
		bodyObj := map[string]string{
			"event":    event,
			"clientId": cid,
			"appSlug":  appSlug,
			"version":  LauncherVersion,
			"os":       runtime.GOOS,
			"arch":     runtime.GOOS + "-" + runtime.GOARCH,
		}
		raw, _ := json.Marshal(bodyObj)
		client := &http.Client{Timeout: 6 * time.Second}
		for _, origin := range candidateOrigins() {
			req, _ := http.NewRequest("POST", origin+"/api/launcher/telemetry", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				return
			}
		}
	}()
}

func findCatalogMeta(slug string) (AppMeta, bool) {
	for _, a := range Catalog {
		if a.Slug == slug {
			return a, true
		}
	}
	return AppMeta{}, false
}

func updateTask(slug string, fn func(*TaskProgress)) {
	mu.Lock()
	defer mu.Unlock()
	tk, ok := tasksMap[slug]
	if !ok {
		tk = &TaskProgress{Slug: slug}
		tasksMap[slug] = tk
	}
	fn(tk)
	if tk.ErrorKey != "" {
		tk.Error = ""
		if tk.Message == "" {
			tk.Message = ""
		}
	}
	if tk.Error != "" && tk.Message == "" && tk.ErrorKey == "" {
		tk.Message = tk.Error
	}
	tk.UpdatedAt = time.Now().UnixMilli()
}

func downloadWithProgress(ctx context.Context, slug, url, destPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ArteCraft-%s-Launcher/%s", osDisplayLabel(), LauncherVersion))
	// Проверка хоста на каждом шаге редиректа: скачиваем только с github.com и
	// *.githubusercontent.com, никаких «зеркал» и сторонних CDN.
	if !isAllowedDownloadURL(url) {
		return "", uerr("err.hostBlocked", url)
	}
	httpClient := githubOnlyClient(0)
	httpClient.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", errNet(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", errHTTP(resp.StatusCode)
	}

	total := resp.ContentLength
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var downloaded int64
	start := time.Now()
	lastTick := start

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", werr
			}
			_, _ = hasher.Write(buf[:n])
			downloaded += int64(n)
			now := time.Now()
			if now.Sub(lastTick) >= 120*time.Millisecond || rerr == io.EOF {
				elapsed := now.Sub(start).Seconds()
				if elapsed < 0.05 {
					elapsed = 0.05
				}
				dlMB := float64(downloaded) / (1024 * 1024)
				totMB := float64(total) / (1024 * 1024)
				if totMB <= 0 {
					totMB = dlMB + 2
				}
				pct := (dlMB / totMB) * 90.0
				spd := dlMB / elapsed
				updateTask(slug, func(tk *TaskProgress) {
					tk.Stage = "downloading"
					tk.DownloadedMB = dlMB
					tk.TotalMB = totMB
					tk.Percent = pct
					tk.SpeedMBps = spd
				})
				lastTick = now
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func detectTarCommonSingleRoot(archivePath string) string {
	f, err := os.Open(archivePath)
	if err != nil {
		return ""
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return ""
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var common string
	count := 0
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(hdr.Name)), "./")
		if clean == "" || clean == "." {
			continue
		}
		parts := strings.SplitN(clean, "/", 2)
		if len(parts) == 1 && hdr.Typeflag != tar.TypeDir {
			// Flat file at root -> do not strip first component
			return ""
		}
		if common == "" {
			common = parts[0]
		} else if parts[0] != common {
			return ""
		}
		count++
	}
	if count == 0 {
		return ""
	}
	return common
}

func extractTarGz(archivePath, destDir string) error {
	stripPrefix := detectTarCommonSingleRoot(archivePath)
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(destDir)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(hdr.Name)), "./")
		if stripPrefix != "" {
			if rel == stripPrefix {
				continue
			}
			rel = strings.TrimPrefix(rel, stripPrefix+"/")
		}
		if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		target := filepath.Join(cleanDest, filepath.FromSlash(rel))
		if !strings.HasPrefix(filepath.Clean(target), cleanDest+string(os.PathSeparator)) && filepath.Clean(target) != cleanDest {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			linkDest := filepath.Clean(filepath.Join(filepath.Dir(target), hdr.Linkname))
			if strings.HasPrefix(linkDest, cleanDest+string(os.PathSeparator)) {
				_ = os.Remove(target)
				_ = os.Symlink(hdr.Linkname, target)
			}
		case tar.TypeReg, tar.TypeRegA:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			mode := os.FileMode(hdr.Mode) & 0777
			if mode == 0 {
				mode = 0644
			}
			if strings.Contains(rel, "bin/") || (hdr.Mode&0111) != 0 {
				mode |= 0755
			}
			wf, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(wf, tr)
			_ = wf.Close()
			if cerr != nil {
				return cerr
			}
		}
	}
	return nil
}

func extractZip(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	cleanDest := filepath.Clean(destDir)
	for _, f := range zr.File {
		target := filepath.Join(cleanDest, filepath.Clean(f.Name))
		if !strings.HasPrefix(filepath.Clean(target), cleanDest+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		wf, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, cerr := io.Copy(wf, rc)
		_ = wf.Close()
		_ = rc.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

func installDMGMacOS(dmgPath, destDir string) (string, error) {
	mntDir, err := os.MkdirTemp("", "artecraft-dmg-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(mntDir)

	// Pass "Y\n" on stdin so SLA-enabled DMGs never hang waiting for interactive prompt
	attachCmd := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-noautoopen", "-mountpoint", mntDir, dmgPath)
	attachCmd.Stdin = strings.NewReader("Y\n")
	if out, err := attachCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("hdiutil attach failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	defer func() {
		_ = exec.Command("hdiutil", "detach", mntDir, "-force", "-quiet").Run()
	}()

	var foundAppPath string
	_ = filepath.WalkDir(mntDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || p == mntDir {
			return nil
		}
		rel, _ := filepath.Rel(mntDir, p)
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".app") {
			foundAppPath = p
			return filepath.SkipDir
		}
		if d.IsDir() && depth >= 1 {
			return filepath.SkipDir
		}
		return nil
	})
	if foundAppPath == "" {
		return "", fmt.Errorf("no .app bundle found inside DMG")
	}

	_ = os.RemoveAll(destDir)
	_ = os.MkdirAll(destDir, 0755)
	targetApp := filepath.Join(destDir, filepath.Base(foundAppPath))
	if out, err := exec.Command("ditto", foundAppPath, targetApp).CombinedOutput(); err != nil {
		return "", fmt.Errorf("ditto copy failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	// Strip Gatekeeper quarantine & ensure Contents/MacOS binaries have +x
	_ = exec.Command("xattr", "-cr", targetApp).Run()
	macOSBinDir := filepath.Join(targetApp, "Contents", "MacOS")
	if entries, rerr := os.ReadDir(macOSBinDir); rerr == nil {
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Chmod(filepath.Join(macOSBinDir, e.Name()), 0755)
			}
		}
	}
	return targetApp, nil
}

// Pick the highest-resolution PNG or scalable SVG icon inside the extracted studio directory
func findAppIconPNG(installDir string) string {
	var best string
	var bestBytes int64 = -1
	var svgFallback string
	_ = filepath.WalkDir(installDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		low := strings.ToLower(d.Name())
		if strings.HasSuffix(low, ".png") {
			if info, ierr := d.Info(); ierr == nil && info.Size() > bestBytes {
				bestBytes = info.Size()
				best = p
			}
		} else if strings.HasSuffix(low, ".svg") && svgFallback == "" {
			svgFallback = p
		}
		return nil
	})
	if best != "" {
		return best
	}
	return svgFallback
}

func locateExecutable(slug, appDir string) string {
	if runtime.GOOS == "darwin" {
		entries, _ := os.ReadDir(appDir)
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".app") {
				return filepath.Join(appDir, e.Name())
			}
		}
	}
	var exactBin, fallbackBin string
	_ = filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if runtime.GOOS == "windows" && base == slug+".exe" {
			exactBin = p
			return io.EOF
		}
		if runtime.GOOS == "linux" {
			if strings.HasSuffix(base, "-cli") || strings.HasSuffix(base, "-server") {
				return nil
			}
			if base == slug {
				exactBin = p
				return io.EOF
			}
			if strings.HasSuffix(base, ".appimage") && fallbackBin == "" {
				fallbackBin = p
			}
		}
		return nil
	})
	if exactBin != "" {
		return exactBin
	}
	return fallbackBin
}

func installWorker() {
	for slug := range installCh {
		meta, ok := findCatalogMeta(slug)
		if !ok {
			continue
		}
		mu.Lock()
		opts := installOptsMap[slug]
		installRoot := appState.InstallRoot
		if strings.TrimSpace(opts.Dir) != "" {
			installRoot = strings.TrimSpace(opts.Dir)
		}
		makeDesk := appState.CreateShortcuts
		if opts.Shortcut != nil {
			makeDesk = *opts.Shortcut
		}
		makeMenu := appState.CreateStartMenu
		if opts.StartMenu != nil {
			makeMenu = *opts.StartMenu
		}
		launchAfter := opts.LaunchAfter != nil && *opts.LaunchAfter
		wantVerify := opts.Verify == nil || *opts.Verify
		if strings.TrimSpace(opts.Args) != "" {
			if appState.Launch == nil {
				appState.Launch = map[string]LaunchConfig{}
			}
			cfg := appState.Launch[slug]
			cfg.Args = strings.TrimSpace(opts.Args)
			appState.Launch[slug] = cfg
			saveStateLocked()
		}
		delete(installOptsMap, slug)
		ctx, cancel := context.WithCancel(context.Background())
		cancelFuncs[slug] = cancel
		mu.Unlock()

		// Ссылка проверяется прямо перед скачиванием: если она «запасная» или
		// устарела, резолвер попробует найти актуальный ассет (без GitHub API,
		// если получится), иначе возьмёт последний удачный вариант из кэша.
		rel := resolveReleaseForInstall(meta)
		updateTask(slug, func(tk *TaskProgress) {
			tk.Stage = "queued"
			tk.MessageKey = "task.preparing"
			tk.Message = ""
			tk.Error = ""
			tk.ErrorKey = ""
		})

		_ = os.MkdirAll(installRoot, 0755)
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("artecraft-%s-%d-%s", slug, os.Getpid(), filepath.Base(rel.AssetName)))
		pkgSha, err := downloadWithProgress(ctx, slug, rel.DownloadURL, tmpFile)
		if err != nil {
			_ = os.Remove(tmpFile)
			stage := "error"
			if ctx.Err() != nil {
				stage = "canceled"
			}
			key, detail := errKeyOf(err)
			if ctx.Err() != nil {
				key, detail = "err.canceled", ""
			}
			updateTask(slug, func(tk *TaskProgress) {
				tk.Stage = stage
				tk.ErrorKey = key
				tk.ErrorDetail = detail
				tk.Error = ""
				tk.Message = ""
				tk.MessageKey = ""
			})
			continue
		}

		updateTask(slug, func(tk *TaskProgress) {
			tk.Stage = "extracting"
			tk.Percent = 95
			tk.Sha256 = pkgSha
			tk.MessageKey = "task.extracting"
		})

		appDir := filepath.Join(installRoot, slug)
		_ = os.RemoveAll(appDir)
		_ = os.MkdirAll(appDir, 0755)

		var execPath string
		lowAsset := strings.ToLower(rel.AssetName)
		switch {
		case strings.HasSuffix(lowAsset, ".dmg"):
			execPath, err = installDMGMacOS(tmpFile, appDir)
		case strings.HasSuffix(lowAsset, ".tar.gz"):
			err = extractTarGz(tmpFile, appDir)
			if err == nil {
				execPath = locateExecutable(slug, appDir)
			}
		case strings.HasSuffix(lowAsset, ".appimage"):
			execPath = filepath.Join(appDir, rel.AssetName)
			if rerr := os.Rename(tmpFile, execPath); rerr != nil {
				// Cross-device rename fallback (/tmp -> ~/.local)
				sf, oerr := os.Open(tmpFile)
				if oerr == nil {
					df, cerr := os.OpenFile(execPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
					if cerr == nil {
						_, err = io.Copy(df, sf)
						_ = df.Close()
					} else {
						err = cerr
					}
					_ = sf.Close()
				} else {
					err = oerr
				}
			}
			if err == nil {
				_ = os.Chmod(execPath, 0755)
			}
		default:
			err = extractZip(tmpFile, appDir)
			if err == nil {
				execPath = locateExecutable(slug, appDir)
			}
		}
		_ = os.Remove(tmpFile)

		if err != nil || execPath == "" {
			if err == nil {
				err = fmt.Errorf("executable not found after extraction")
			}
			updateTask(slug, func(tk *TaskProgress) {
				tk.Stage = "error"
				tk.Error = err.Error()
			})
			continue
		}
		if st, serr := os.Stat(execPath); serr == nil && !st.IsDir() {
			_ = os.Chmod(execPath, 0755)
		}

		inst := InstalledApp{
			Slug:          slug,
			Version:       rel.Version,
			InstallDir:    appDir,
			InstallFolder: appDir,
			ExecPath:      execPath,
			SizeMB:        rel.SizeMB,
			InstalledAt:   time.Now().UTC().Format(time.RFC3339),
		}

		mu.Lock()
		appState.Installed[slug] = inst
		saveStateLocked()
		mu.Unlock()

		if makeDesk || makeMenu {
			_ = createPlatformShortcuts(meta, inst, makeDesk, makeMenu)
		}

		updateTask(slug, func(tk *TaskProgress) {
			tk.Stage = "done"
			tk.Percent = 100
			if wantVerify {
				tk.Sha256 = pkgSha
			} else {
				tk.Sha256 = ""
			}
			tk.Error = ""
			tk.Message = ""
		})
		if launchAfter {
			_ = launchInstalledBinary(appState.Installed[slug])
		}
		sendTelemetry("app_install", slug)
	}
}

func computeSelfSHA256() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func isClientStillPolling() bool {
	if !hasSeenClient.Load() {
		return false
	}
	return (time.Now().UnixMilli() - lastPollUnixMs.Load()) < 5000
}

func activeInstanceFilePath() string {
	return filepath.Join(getAppDataDir(), "active-hub.url")
}

// Used by browser-fallback modes (!cgo on macOS or minimal Linux without WebKitGTK/Chromium)
// so closing the browser tab cleanly terminates the local HTTP server instead of hanging forever.
func waitBrowserSessionOrSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	startTime := time.Now()
	for {
		select {
		case <-sigCh:
			return
		case <-ticker.C:
			if hasSeenClient.Load() {
				idleMs := time.Now().UnixMilli() - lastPollUnixMs.Load()
				mu.Lock()
				busy := false
				for _, tk := range tasksMap {
					if tk.Stage == "downloading" || tk.Stage == "extracting" || tk.Stage == "queued" {
						busy = true
						break
					}
				}
				mu.Unlock()
				if !busy && idleMs > 18000 {
					return
				}
			} else if time.Since(startTime) > 90*time.Second {
				return
			}
		}
	}
}

func buildHTTPMux() *http.ServeMux {
	mux := http.NewServeMux()
	registerMarketRoutes(mux)

	uiSub, _ := fs.Sub(embeddedUI, "ui")
	fileSrv := http.FileServer(http.FS(uiSub))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		lastPollUnixMs.Store(time.Now().UnixMilli())
		hasSeenClient.Store(true)
		mu.Lock()
		payload := map[string]any{
			"custom":          appState.Custom,
			"launchOpts":      appState.Launch,
			"siteAccount":     map[string]any{"signedIn": appState.SiteToken != "", "email": appState.SiteEmail, "name": appState.SiteName, "handle": appState.SiteHandle, "deviceId": DeviceID()[:10]},
			"hasGithubToken":  appState.GithubToken != "",
			"launcherVersion": LauncherVersion,
			"os":              runtime.GOOS,
			"osLabel":         osDisplayLabel(),
			"arch":            archDisplayLabel(),
			"locale":          appState.Locale,
			"installRoot":     appState.InstallRoot,
			"autoStart":       appState.AutoStart,
			"autoStartActive": autoStartEnabled(),
			"autoStartCmd":    autoStartCommandLine(),
			"traySupported":   traySupported(),
			"closeToTray":     appState.CloseToTray,
			"createShortcuts": appState.CreateShortcuts,
			"createStartMenu": appState.CreateStartMenu,
			"lowSpecMode":     appState.LowSpecMode,
			"catalog":         Catalog,
			"installed":       appState.Installed,
			"releases":        releasesMap,
			"tasks":           tasksMap,
			"selfUpdate":      selfUpd,
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/api/install", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slugs   []string       `json:"slugs"`
			Options InstallOptions `json:"options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if locked, st := launcherLockedForUpdates(); locked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "queued": 0, "errorKey": "err.updateRequired", "selfUpdate": st,
			})
			return
		}
		queued := 0
		for _, sl := range body.Slugs {
			if _, ok := findCatalogMeta(sl); !ok {
				continue
			}
			mu.Lock()
			installOptsMap[sl] = body.Options
			mu.Unlock()
			updateTask(sl, func(tk *TaskProgress) {
				tk.Stage = "queued"
				tk.Percent = 2
				tk.Error = ""
				tk.Message = ""
			})
			installCh <- sl
			queued++
		}
		writeJSON(w, map[string]any{"ok": true, "queued": queued})
	})

	// Проверка папки установки: существует ли, можно ли писать. Нужна, чтобы в UI
	// сразу подсветить ошибку, а не падать посреди распаковки.
	mux.HandleFunc("/api/check-dir", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		path := strings.TrimSpace(body.Path)
		if path == "" {
			writeJSON(w, map[string]any{"ok": false, "error": "path is empty"})
			return
		}
		if !filepath.IsAbs(path) {
			writeJSON(w, map[string]any{"ok": false, "error": "path must be absolute"})
			return
		}
		existed := true
		if st, err := os.Stat(path); err != nil {
			existed = false
		} else if !st.IsDir() {
			writeJSON(w, map[string]any{"ok": false, "error": "not a directory"})
			return
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			writeJSON(w, map[string]any{"ok": false, "writable": false, "error": err.Error()})
			return
		}
		probe := filepath.Join(path, ".artecraft-write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0644); err != nil {
			writeJSON(w, map[string]any{"ok": false, "writable": false, "error": err.Error()})
			return
		}
		_ = os.Remove(probe)
		writeJSON(w, map[string]any{"ok": true, "writable": true, "exists": existed, "path": path})
	})

	mux.HandleFunc("/api/cancel", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if fn, ok := cancelFuncs[body.Slug]; ok {
			fn()
		}
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/launch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		inst, ok := appState.Installed[body.Slug]
		mu.Unlock()
		if ok && inst.ExecPath != "" {
			if err := launchInstalledBinary(inst); err != nil {
				w.WriteHeader(500)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
				return
			}
			sendTelemetry("app_launch", body.Slug)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/uninstall", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		inst, ok := appState.Installed[body.Slug]
		if ok {
			if inst.InstallDir != "" {
				_ = os.RemoveAll(inst.InstallDir)
			}
			delete(appState.Installed, body.Slug)
			saveStateLocked()
		}
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/shortcut", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body.Slug {
		case "__launcher__":
			_ = createSelfDesktopShortcut()
		case "__all__":
			mu.Lock()
			snapshot := make([]InstalledApp, 0, len(appState.Installed))
			for _, inst := range appState.Installed {
				snapshot = append(snapshot, inst)
			}
			mu.Unlock()
			for _, inst := range snapshot {
				if meta, mOk := findCatalogMeta(inst.Slug); mOk {
					_ = createPlatformShortcuts(meta, inst, true, true)
				}
			}
		default:
			mu.Lock()
			inst, ok := appState.Installed[body.Slug]
			mu.Unlock()
			if meta, mOk := findCatalogMeta(body.Slug); ok && mOk {
				_ = createPlatformShortcuts(meta, inst, true, true)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/open-folder", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		target := appState.InstallRoot
		if inst, ok := appState.Installed[body.Slug]; ok && inst.InstallDir != "" {
			target = inst.InstallDir
		}
		mu.Unlock()
		_ = openFolderNative(target)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/pick-folder", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cur := appState.InstallRoot
		mu.Unlock()
		if picked := pickFolderNative(cur); picked != "" {
			mu.Lock()
			appState.InstallRoot = picked
			_ = os.MkdirAll(picked, 0755)
			saveStateLocked()
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&patch)
		mu.Lock()
		if v, ok := patch["installRoot"].(string); ok && strings.TrimSpace(v) != "" {
			appState.InstallRoot = strings.TrimSpace(v)
			_ = os.MkdirAll(appState.InstallRoot, 0755)
		}
		if v, ok := patch["locale"].(string); ok {
			appState.Locale = v
		}
		autoStartErr := ""
		if v, ok := patch["autoStart"].(bool); ok {
			if err := setPlatformAutoStart(v); err != nil {
				// Настройка не применилась — не делаем вид, что всё хорошо.
				autoStartErr = err.Error()
			} else {
				appState.AutoStart = v
			}
		}
		trayRestartNeeded := false
		if v, ok := patch["closeToTray"].(bool); ok {
			appState.CloseToTray = v
			setTrayHideOnClose(v)
			if v {
				// Трей мог быть не создан при старте — включаем без перезапуска.
				mainWindowMu.Lock()
				win := mainWindow
				term := mainTerminate
				mainWindowMu.Unlock()
				if win != nil && term != nil && !installTray(win, true, term) {
					trayRestartNeeded = true
				}
			}
		}
		if v, ok := patch["createShortcuts"].(bool); ok {
			appState.CreateShortcuts = v
		}
		if v, ok := patch["createStartMenu"].(bool); ok {
			appState.CreateStartMenu = v
		}
		if v, ok := patch["lowSpecMode"].(bool); ok {
			appState.LowSpecMode = v
		}
		saveStateLocked()
		mu.Unlock()
		resp := map[string]any{
			"ok":                true,
			"autoStart":         appState.AutoStart,
			"autoStartActive":   autoStartEnabled(),
			"closeToTray":       appState.CloseToTray,
			"traySupported":     traySupported(),
			"trayRestartNeeded": trayRestartNeeded,
		}
		if autoStartErr != "" {
			resp["ok"] = false
			resp["errorKey"] = "err.autostartFailed"
			resp["errorDetail"] = autoStartErr
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/open-url", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(body.URL, "https://") || strings.HasPrefix(body.URL, "http://") {
			_ = openExternalURL(body.URL)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/open-update-page", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		u := selfUpd.DownloadURL
		mu.Unlock()
		if u == "" {
			u = "https://getartecraft.com/download"
		}
		sendTelemetry("update_click", "")
		_ = openExternalURL(u)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/check-launcher-update", func(w http.ResponseWriter, r *http.Request) {
		st := checkRemoteLauncherUpdate()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})

	mux.HandleFunc("/api/refresh-releases", func(w http.ResponseWriter, r *http.Request) {
		go resolveAllReleases()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// Свернуть окно в трей / показать обратно — для одноимённой кнопки в UI.
	mux.HandleFunc("/api/tray/hide", func(w http.ResponseWriter, r *http.Request) {
		hideMainWindow()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/tray/show", func(w http.ResponseWriter, r *http.Request) {
		showMainWindow()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		_ = os.Remove(activeInstanceFilePath())
		_, _ = w.Write([]byte(`{"ok":true}`))
		go func() {
			time.Sleep(120 * time.Millisecond)
			os.Exit(0)
		}()
	})

	mux.Handle("/", fileSrv)
	return mux
}

func main() {
	if RunCLIIfRequested(os.Args[1:]) {
		return
	}
	selfTest := flag.Bool("self-test", false, "Run automated headless health check and exit")
	startInTray := flag.Bool("tray", false, "Start minimized to the system tray (used by autostart)")
	flag.Parse()

	// CLI-режим: artecraft list / install / launch / --help
	_ = selfTest

	loadState()

	// Single-instance check: if ArteCraft is already running locally, focus/open it instead of spawning a duplicate
	if !*selfTest {
		if prevBytes, rerr := os.ReadFile(activeInstanceFilePath()); rerr == nil {
			prevURL := strings.TrimSpace(string(prevBytes))
			if strings.HasPrefix(prevURL, "http://127.0.0.1:") {
				cl := &http.Client{Timeout: 350 * time.Millisecond}
				if resp, gerr := cl.Get(prevURL + "/api/status"); gerr == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == 200 {
						_ = openExternalURL(prevURL)
						os.Exit(0)
					}
				}
			}
		}
	}

	for _, app := range Catalog {
		releasesMap[app.Slug] = fallbackReleaseForOS(app)
	}
	go installWorker()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to bind local UI server: %v\n", err)
		os.Exit(1)
	}
	localURL := "http://" + ln.Addr().String()
	srv := &http.Server{Handler: buildHTTPMux()}
	go func() { _ = srv.Serve(ln) }()

	if *selfTest {
		resp, err := http.Get(localURL + "/api/status")
		if err != nil || resp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "self-test failed on /api/status: %v\n", err)
			os.Exit(1)
		}
		var st map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&st)
		_ = resp.Body.Close()
		uiResp, err := http.Get(localURL + "/")
		if err != nil || uiResp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "self-test failed on /: %v\n", err)
			os.Exit(1)
		}
		_ = uiResp.Body.Close()
		shotResp, err := http.Get(localURL + "/shots/photocraft.jpg")
		if err != nil || shotResp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "self-test failed on /shots/photocraft.jpg: %v\n", err)
			os.Exit(1)
		}
		_ = shotResp.Body.Close()
		selfSha := computeSelfSHA256()
		if len(selfSha) > 16 {
			selfSha = selfSha[:16]
		}
		fmt.Printf("[PASS] ArteCraft %s (%s/%s) self-test OK | catalog=%d | selfSHA256=%s\n",
			LauncherVersion, runtime.GOOS, runtime.GOARCH, len(Catalog), selfSha)
		os.Exit(0)
	}

	_ = os.WriteFile(activeInstanceFilePath(), []byte(localURL), 0644)
	defer os.Remove(activeInstanceFilePath())

	// Автозапуск идемпотентен: путь к exe мог измениться (перетащили папку),
	// поэтому при каждом старте запись в реестре/autostart обновляется.
	if appState.AutoStart {
		_ = setPlatformAutoStart(true)
	}

	go resolveAllReleases()
	go checkRemoteLauncherUpdate()
	// Раз в полчаса перепроверяем состояние обновления: если владелец поднял
	// minVersion на сайте, открытый лаунчер сам уйдёт в блокировку.
	go func() {
		t := time.NewTicker(30 * time.Minute)
		for range t.C {
			checkRemoteLauncherUpdate()
		}
	}()
	sendTelemetry("launch", "")

	if *startInTray {
		startHiddenInTray = true
	}

	runPlatformDesktopUI(localURL)
}
