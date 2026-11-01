package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildSyntheticTarGz(t *testing.T, entries map[string]string, symlinks map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range entries {
		mode := int64(0644)
		if strings.Contains(name, "bin/") || strings.HasPrefix(content, "#!") {
			mode = 0755
		}
		hdr := &tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s): %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	for linkPath, target := range symlinks {
		hdr := &tar.Header{
			Name:     linkPath,
			Linkname: target,
			Mode:     0777,
			Typeflag: tar.TypeSymlink,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader symlink(%s): %v", linkPath, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTarGzNestedSymlinksAndIcons(t *testing.T) {
	raw := buildSyntheticTarGz(t, map[string]string{
		"wordcraft-0.11.0-linux-x86_64/bin/wordcraft-cli": "#!/bin/sh\necho cli\n",
		"wordcraft-0.11.0-linux-x86_64/bin/wordcraft":     "#!/bin/sh\necho gui\n",
		"wordcraft-0.11.0-linux-x86_64/lib/libfoo.so.1.0": "ELF_SHARED_OBJ",
		"wordcraft-0.11.0-linux-x86_64/icons/64x64.png":   strings.Repeat("a", 120),
		"wordcraft-0.11.0-linux-x86_64/icons/512x512.png": strings.Repeat("b", 4096),
		"wordcraft-0.11.0-linux-x86_64/icons/128x128.png": strings.Repeat("c", 512),
	}, map[string]string{
		"wordcraft-0.11.0-linux-x86_64/lib/libfoo.so.1": "libfoo.so.1.0",
		"wordcraft-0.11.0-linux-x86_64/lib/evil_escape": "../../../../etc/passwd",
	})

	tmpArc := filepath.Join(t.TempDir(), "wordcraft.tar.gz")
	if err := os.WriteFile(tmpArc, raw, 0644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "installed-wordcraft")
	if err := extractTarGz(tmpArc, dest); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}

	// Verify symlink preserved inside cleanDest (on Unix; Windows NTFS symlinks require elevated Developer Mode)
	if runtime.GOOS != "windows" {
		symTarget, err := os.Readlink(filepath.Join(dest, "lib", "libfoo.so.1"))
		if err != nil || symTarget != "libfoo.so.1.0" {
			t.Fatalf("expected symlink libfoo.so.1 -> libfoo.so.1.0, got %q (err=%v)", symTarget, err)
		}
	}
	// Verify path traversal symlink was blocked on ALL OSes
	if _, err := os.Lstat(filepath.Join(dest, "lib", "evil_escape")); err == nil {
		t.Fatalf("expected path-traversal symlink to be blocked!")
	}

	bestIcon := findAppIconPNG(dest)
	if filepath.Base(bestIcon) != "512x512.png" {
		t.Fatalf("expected highest-res 512x512.png icon, got %q", bestIcon)
	}

	if runtime.GOOS == "linux" {
		exe := locateExecutable("wordcraft", dest)
		if !strings.HasSuffix(filepath.ToSlash(exe), "/bin/wordcraft") {
			t.Fatalf("locateExecutable picked wrong binary (must skip -cli): %q", exe)
		}
	}
}

func TestTarGzFlatArchiveNotCorrupted(t *testing.T) {
	raw := buildSyntheticTarGz(t, map[string]string{
		"photocraft": "#!/bin/sh\necho flat\n",
		"README.txt": "hello world",
	}, nil)
	tmpArc := filepath.Join(t.TempDir(), "flat.tar.gz")
	_ = os.WriteFile(tmpArc, raw, 0644)
	dest := filepath.Join(t.TempDir(), "flat-out")
	if err := extractTarGz(tmpArc, dest); err != nil {
		t.Fatalf("extractTarGz flat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "photocraft")); err != nil {
		t.Fatalf("flat root binary missing after extraction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.txt")); err != nil {
		t.Fatalf("flat README.txt missing after extraction: %v", err)
	}
}

func TestEndToEndInstallLaunchShortcutUninstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific e2e pipeline test")
	}
	fakeHome := t.TempDir()
	// Скачивание разрешено только с GitHub; тесту нужен собственный mirror.
	t.Setenv("ARTECRAFT_ALLOW_LOCAL_MIRROR", "1")
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))
	_ = os.MkdirAll(filepath.Join(fakeHome, "Desktop"), 0755)

	launchProof := filepath.Join(fakeHome, "photocraft-launched.txt")
	scriptBody := "#!/bin/sh\nprintf 'APPDIR=%s|LD=%s' \"$APPDIR\" \"$LD_LIBRARY_PATH\" > " + launchProof + "\n"
	pkgBytes := buildSyntheticTarGz(t, map[string]string{
		"photocraft-0.16.0-linux-x86_64/bin/photocraft":     scriptBody,
		"photocraft-0.16.0-linux-x86_64/bin/photocraft-cli": "#!/bin/sh\nexit 1\n",
		"photocraft-0.16.0-linux-x86_64/icons/256x256.png":  strings.Repeat("P", 2048),
	}, nil)

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(pkgBytes)
	}))
	defer mirror.Close()

	loadState()
	go installWorker()

	mu.Lock()
	releasesMap["photocraft"] = RemoteReleaseInfo{
		Version:     "0.16.0",
		AssetName:   "photocraft-0.16.0-linux-x86_64.tar.gz",
		DownloadURL: mirror.URL + "/photocraft-0.16.0-linux-x86_64.tar.gz",
		SizeMB:      1.2,
	}
	mu.Unlock()

	srv := httptest.NewServer(buildHTTPMux())
	defer srv.Close()

	// 1. Verify /api/status + embedded assets
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("/api/status failed: %v", err)
	}
	var st struct {
		LauncherVersion string    `json:"launcherVersion"`
		Catalog         []AppMeta `json:"catalog"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	_ = resp.Body.Close()
	if st.LauncherVersion != LauncherVersion || len(st.Catalog) != 13 {
		t.Fatalf("unexpected status version=%s catalog=%d", st.LauncherVersion, len(st.Catalog))
	}

	for _, a := range st.Catalog {
		sr, serr := http.Get(srv.URL + "/shots/" + a.Slug + ".jpg")
		if serr != nil || sr.StatusCode != 200 {
			t.Fatalf("missing screenshot for %s: err=%v", a.Slug, serr)
		}
		_ = sr.Body.Close()
	}

	// 2. Trigger install of photocraft
	instRes, err := http.Post(srv.URL+"/api/install", "application/json", strings.NewReader(`{"slugs":["photocraft"]}`))
	if err != nil || instRes.StatusCode != 200 {
		t.Fatalf("/api/install failed: %v", err)
	}
	_ = instRes.Body.Close()

	deadline := time.Now().Add(10 * time.Second)
	installedOK := false
	for time.Now().Before(deadline) {
		r, _ := http.Get(srv.URL + "/api/status")
		var cur struct {
			Installed map[string]InstalledApp `json:"installed"`
			Tasks     map[string]TaskProgress `json:"tasks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&cur)
		_ = r.Body.Close()
		if tk, ok := cur.Tasks["photocraft"]; ok && tk.Stage == "error" {
			t.Fatalf("install failed with error: %s", tk.Error)
		}
		if _, ok := cur.Installed["photocraft"]; ok {
			installedOK = true
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !installedOK {
		t.Fatalf("photocraft did not finish installing within timeout")
	}

	// 3. Test polluted AppImage environment sanitization during /api/launch
	t.Setenv("APPDIR", "/tmp/fake_appimage_mount")
	t.Setenv("LD_LIBRARY_PATH", "/tmp/fake_appimage_mount/usr/lib:/usr/local/customlib")

	lRes, err := http.Post(srv.URL+"/api/launch", "application/json", strings.NewReader(`{"slug":"photocraft"}`))
	if err != nil || lRes.StatusCode != 200 {
		t.Fatalf("/api/launch failed: %v", err)
	}
	_ = lRes.Body.Close()

	var proofBytes []byte
	for i := 0; i < 30; i++ {
		if b, rerr := os.ReadFile(launchProof); rerr == nil && len(b) > 0 {
			proofBytes = b
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if string(proofBytes) != "APPDIR=|LD=/usr/local/customlib" {
		t.Fatalf("child env not sanitized properly! got %q", string(proofBytes))
	}

	// 4. Test __all__ and __launcher__ shortcuts
	for _, sc := range []string{"__all__", "__launcher__"} {
		scRes, serr := http.Post(srv.URL+"/api/shortcut", "application/json", strings.NewReader(`{"slug":"`+sc+`"}`))
		if serr != nil || scRes.StatusCode != 200 {
			t.Fatalf("/api/shortcut %s failed: %v", sc, serr)
		}
		_ = scRes.Body.Close()
	}
	if _, err := os.Stat(filepath.Join(fakeHome, "Desktop", "artecraft-photocraft.desktop")); err != nil {
		t.Fatalf("expected Desktop shortcut for photocraft: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fakeHome, "Desktop", "ArteCraft.desktop")); err != nil {
		t.Fatalf("expected Desktop shortcut for ArteCraft launcher: %v", err)
	}

	// 5. Test /api/settings (autostart enable & disable)
	setRes, _ := http.Post(srv.URL+"/api/settings", "application/json", strings.NewReader(`{"autoStart":true,"locale":"ru"}`))
	_ = setRes.Body.Close()
	if _, err := os.Stat(filepath.Join(fakeHome, ".config", "autostart", "artecraft.desktop")); err != nil {
		t.Fatalf("expected autostart .desktop file to be created: %v", err)
	}

	// 6. Test /api/uninstall
	unRes, err := http.Post(srv.URL+"/api/uninstall", "application/json", strings.NewReader(`{"slug":"photocraft"}`))
	if err != nil || unRes.StatusCode != 200 {
		t.Fatalf("/api/uninstall failed: %v", err)
	}
	_ = unRes.Body.Close()
	mu.Lock()
	_, stillThere := appState.Installed["photocraft"]
	mu.Unlock()
	if stillThere {
		t.Fatalf("photocraft still in appState.Installed after uninstall")
	}
}
