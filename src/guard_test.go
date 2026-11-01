package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guard_test.go — тесты-«сторожа» на две жалобы пользователя:
//  1. в интерфейсе всплывали русские строки при английском/немецком языке;
//  2. лаунчер мог скачивать не только с GitHub.

// TestNoCyrillicInGoStringsAndHTML ловит русские строки в Go-коде и в разметке.
// Комментарии и тестовые файлы не считаются: важно, чтобы пользователю не
// показывался русский текст в обход i18n.
func TestNoCyrillicInGoStringsAndHTML(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// Пути и имена папок ОС (например, «Рабочий стол») — не текст
			// интерфейса, их помечают i18n-ignore в том же комментарии.
			if strings.Contains(line, "i18n-ignore") {
				continue
			}
			// отрезаем комментарий в конце строки
			if idx := strings.Index(line, "//"); idx >= 0 {
				line = line[:idx]
			}
			if !strings.Contains(line, "\"") {
				continue
			}
			if hasCyrillic(line) {
				t.Fatalf("%s:%d — русская строка в Go-коде. Пользовательский текст должен идти ключом i18n (см. messages.go): %s",
					name, i+1, strings.TrimSpace(line))
			}
		}
	}

	html, err := os.ReadFile(filepath.Join("ui", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"Настройки", "Загрузки", "Приложения", "Поиск", "Установить"} {
		if strings.Contains(string(html), word) {
			t.Fatalf("index.html: найдена захардкоженная строка %q — разметка должна содержать только data-i18n", word)
		}
	}
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' || r == 'Ё' || r == 'ё' {
			return true
		}
	}
	return false
}

// TestDownloadHostsGithubOnly — правило «скачиваем только с github.com и
// *.githubusercontent.com», включая похожие домены-обманки.
func TestDownloadHostsGithubOnly(t *testing.T) {
	allowed := []string{
		"https://github.com/storytold/photocraft/releases/download/v0.18.0/photocraft-0.18.0-windows-x64-portable.zip",
		"https://objects.githubusercontent.com/github-production-release-asset/x",
		"https://release-assets.githubusercontent.com/y",
		"https://api.github.com/repos/storytold/photocraft/releases/latest",
	}
	for _, u := range allowed {
		if !isAllowedDownloadURL(u) {
			t.Fatalf("должен быть разрешён: %s", u)
		}
	}
	blocked := []string{
		"http://github.com/x.zip",
		"https://evil.com/github.com/x.zip",
		"https://github.com.evil.com/x.zip",
		"https://evilgithubusercontent.com/x.zip",
		"https://cdn.mirror-host.net/x.zip",
		"ftp://github.com/x.zip",
	}
	for _, u := range blocked {
		if isAllowedDownloadURL(u) {
			t.Fatalf("должен быть запрещён: %s", u)
		}
	}
}

// TestTaskMessagesAreKeys — стадии загрузки уходят в UI ключами, а не текстом.
func TestTaskMessagesAreKeys(t *testing.T) {
	updateTask("guard-test", func(tk *TaskProgress) {
		tk.Stage = "download"
		tk.Message = ""
		tk.MessageKey = "task.downloading"
	})
	if tk := tasksMap["guard-test"]; tk.MessageKey != "task.downloading" || tk.Message != "" {
		t.Fatalf("ожидался ключ без текста, получено key=%q message=%q", tk.MessageKey, tk.Message)
	}
	key, detail := errKeyOf(uerr("err.hostBlocked", "cdn.example.com"))
	if key != "err.hostBlocked" || detail != "cdn.example.com" {
		t.Fatalf("errKeyOf вернул %q/%q", key, detail)
	}
	if k, _ := errKeyOf(errHTTP(403)); k != "err.rateLimit" {
		t.Fatalf("403 должен давать err.rateLimit, получено %q", k)
	}
	if k, _ := errKeyOf(errHTTP(404)); k != "err.assetMissing" {
		t.Fatalf("404 должен давать err.assetMissing, получено %q", k)
	}
}

// TestI18nCoversServerKeys — каждый ключ, который может прийти из Go,
// обязан быть в словаре на всех 15 языках.
func TestI18nCoversServerKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("ui", "i18n.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(raw)
	keys := []string{
		"task.preparing", "task.downloading", "task.extracting", "task.done",
		"err.generic", "err.network", "err.timeout", "err.dns", "err.canceled",
		"err.rateLimit", "err.assetMissing", "err.httpStatus", "err.hostBlocked",
		"err.tooManyRedirects", "err.onlyGithub", "err.emptyLink", "err.badLink",
		"err.repoFormat", "err.repoNotFound", "err.badRequest", "err.notFound",
		"err.notInstalled", "err.loginFailed", "err.catalogUnavailable",
		"err.siteUnavailable", "err.loginRequired", "err.rpmUnsupported",
		"err.execNotFound", "err.noAsset", "err.packageCorrupt", "err.autostartFailed",
		"warn.noAssets", "warn.noOsAsset", "warn.noLicense", "warn.archived",
		"warn.stale", "warn.fewStars", "warn.unverified",
		"settings.autostartOn", "settings.autostartOff", "settings.trayUnsupported",
		"settings.trayHideNow", "settings.trayHidden", "settings.trayRestart",
	}
	for _, k := range keys {
		marker := `DICT["` + k + `"]`
		idx := strings.Index(js, marker)
		if idx < 0 {
			t.Fatalf("в i18n.js нет ключа %q", k)
		}
		end := strings.Index(js[idx:], "];")
		if end < 0 {
			t.Fatalf("ключ %q не закрыт", k)
		}
		body := js[idx : idx+end]
		if got := strings.Count(body, `","`) + 1; got != 15 {
			t.Fatalf("ключ %q переведён на %d языков, нужно 15", k, got)
		}
	}
}
