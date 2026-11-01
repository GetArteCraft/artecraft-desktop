package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проверка эндпоинта проверки папки: относительный путь отклоняется,
// абсолютный — создаётся и подтверждается как writable.
func TestCheckDirEndpoint(t *testing.T) {
	srv := httptest.NewServer(buildHTTPMux())
	defer srv.Close()

	post := func(body string) map[string]any {
		resp, err := http.Post(srv.URL+"/api/check-dir", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/check-dir: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	rel := post(`{"path":"relative/dir"}`)
	if rel["ok"] != false {
		t.Fatalf("relative path must be rejected, got %v", rel)
	}

	abs := filepath.Join(t.TempDir(), "apps")
	res := post(`{"path":"` + abs + `"}`)
	if res["ok"] != true || res["writable"] != true {
		t.Fatalf("absolute writable dir must be accepted, got %v", res)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("dir was not created: %v", err)
	}
}

// Параметры из диалога установки должны доезжать до воркера (папка, ярлыки, аргументы).
func TestInstallOptionsReachWorker(t *testing.T) {
	mu.Lock()
	installOptsMap = map[string]InstallOptions{}
	mu.Unlock()

	srv := httptest.NewServer(buildHTTPMux())
	defer srv.Close()

	body := `{"slugs":["photocraft"],"options":{"dir":"` + filepath.Join(t.TempDir(), "apps") +
		`","shortcut":false,"startMenu":true,"launchAfter":true,"args":"--lang=de","verify":false}}`
	resp, err := http.Post(srv.URL+"/api/install", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/install: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("install returned %d", resp.StatusCode)
	}

	mu.Lock()
	opts, ok := installOptsMap["photocraft"]
	mu.Unlock()
	if !ok {
		t.Fatal("install options were not stored for the slug")
	}
	if opts.Args != "--lang=de" {
		t.Fatalf("args not propagated: %q", opts.Args)
	}
	if opts.Shortcut == nil || *opts.Shortcut {
		t.Fatalf("shortcut=false not propagated: %v", opts.Shortcut)
	}
	if opts.StartMenu == nil || !*opts.StartMenu {
		t.Fatalf("startMenu=true not propagated: %v", opts.StartMenu)
	}
	if opts.Verify == nil || *opts.Verify {
		t.Fatalf("verify=false not propagated: %v", opts.Verify)
	}

	// дренаж очереди, чтобы фоновые горутины теста не висели
	for len(installCh) > 0 {
		<-installCh
	}
}

// UI отдаётся целиком: разметка, стили, i18n на 15 языков и логика.
func TestEmbeddedUILocalesServed(t *testing.T) {
	srv := httptest.NewServer(buildHTTPMux())
	defer srv.Close()

	for _, path := range []string{"/", "/styles.css", "/app.js", "/i18n.js", "/logo.png", "/favicon.png"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s → %d", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	srcBytes, _ := io.ReadAll(resp.Body)
	src := string(srcBytes)
	if !strings.Contains(src, "settings.lang") || !strings.Contains(src, "العربية") {
		t.Fatal("i18n.js does not contain the 15-locale dictionary")
	}

	respHTML, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = respHTML.Body.Close() }()
	htmlBytes, _ := io.ReadAll(respHTML.Body)
	html := string(htmlBytes)
	if strings.Contains(html, "Поиск") || strings.Contains(html, "Гость") {
		t.Fatal("index.html must not contain hardcoded Russian strings — everything goes through i18n")
	}
}
