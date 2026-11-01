//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Strip AppImage runtime variables before launching external browsers or installed Rust GPU apps
// so child binaries use clean host Mesa/Vulkan/GTK libraries without symbol lookup crashes.
func cleanHostEnv() []string {
	blockedPrefixes := []string{
		"APPDIR=",
		"OWD=",
		"ARGV0=",
		"GDK_PIXBUF_MODULE_FILE=",
		"GDK_PIXBUF_MODULEDIR=",
		"GTK_PATH=",
		"GTK_IM_MODULE_FILE=",
		"GIO_MODULE_DIR=",
		"QT_PLUGIN_PATH=",
	}
	appDir := os.Getenv("APPDIR")
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		skip := false
		for _, pref := range blockedPrefixes {
			if strings.HasPrefix(kv, pref) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") && appDir != "" {
			parts := strings.Split(strings.TrimPrefix(kv, "LD_LIBRARY_PATH="), ":")
			var kept []string
			for _, p := range parts {
				if p != "" && !strings.HasPrefix(p, appDir) {
					kept = append(kept, p)
				}
			}
			if len(kept) > 0 {
				out = append(out, "LD_LIBRARY_PATH="+strings.Join(kept, ":"))
			}
			continue
		}
		out = append(out, kv)
	}
	return out
}

func resolveSelfLinuxExec() string {
	if appImage := strings.TrimSpace(os.Getenv("APPIMAGE")); appImage != "" {
		return appImage
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func runPlatformDesktopUI(localURL string) {
	// Tier 1: Native GTK3 + WebKit2GTK window via runtime dlopen (zero hard .so dependency!)
	if os.Getenv("ARTECRAFT_NO_WEBKIT") != "1" && tryNativeLinuxWebView(localURL) {
		return
	}

	// Tier 2: Dedicated frameless app-mode window via Chromium/Chrome/Brave/Edge/Vivaldi/Opera
	browsers := []string{
		"chromium",
		"chromium-browser",
		"google-chrome",
		"google-chrome-stable",
		"brave-browser",
		"microsoft-edge",
		"microsoft-edge-stable",
		"vivaldi",
		"opera",
	}
	for _, b := range browsers {
		if bin, err := exec.LookPath(b); err == nil {
			profileDir := filepath.Join(getAppDataDir(), "app-window-profile")
			_ = os.MkdirAll(profileDir, 0755)
			cmd := exec.Command(bin,
				"--app="+localURL,
				"--class=ArteCraft",
				"--window-size=1280,820",
				"--user-data-dir="+profileDir,
				"--no-first-run",
				"--no-default-browser-check",
			)
			cmd.Env = cleanHostEnv()
			started := time.Now()
			if cmd.Start() == nil {
				_ = cmd.Wait()
				if time.Since(started) < 6*time.Second || isClientStillPolling() {
					waitBrowserSessionOrSignal()
				}
				return
			}
		}
	}

	// Tier 3: Firefox / xdg-open fallback with automatic session-close termination
	fmt.Printf("ArteCraft Desktop Hub running at %s\n", localURL)
	opened := false
	for _, fb := range []string{"xdg-open", "firefox", "firefox-esr"} {
		if bin, err := exec.LookPath(fb); err == nil {
			cmd := exec.Command(bin, localURL)
			cmd.Env = cleanHostEnv()
			if cmd.Start() == nil {
				opened = true
				break
			}
		}
	}
	if !opened {
		fmt.Fprintf(os.Stderr, "Open %s in your browser to use ArteCraft.\n", localURL)
	}
	waitBrowserSessionOrSignal()
}

func launchInstalledBinary(inst InstalledApp) error {
	args := []string{}
	// If launching an AppImage on Ubuntu 24.04+ where libfuse2 might be absent,
	// --appimage-extract-and-run guarantees execution without FUSE!
	if strings.HasSuffix(strings.ToLower(inst.ExecPath), ".appimage") {
		if _, err := os.Stat("/usr/lib/x86_64-linux-gnu/libfuse.so.2"); err != nil {
			if _, err2 := os.Stat("/lib/x86_64-linux-gnu/libfuse.so.2"); err2 != nil {
				args = append(args, "--appimage-extract-and-run")
			}
		}
	}
	cmd := exec.Command(inst.ExecPath, args...)
	cmd.Dir = inst.InstallDir
	cmd.Env = cleanHostEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func openExternalURL(u string) error {
	cmd := exec.Command("xdg-open", u)
	cmd.Env = cleanHostEnv()
	return cmd.Start()
}

func openFolderNative(dir string) error {
	_ = os.MkdirAll(dir, 0755)
	cmd := exec.Command("xdg-open", dir)
	cmd.Env = cleanHostEnv()
	return cmd.Start()
}

func pickFolderNative(current string) string {
	_ = os.MkdirAll(current, 0755)
	if _, err := exec.LookPath("zenity"); err == nil {
		cmd := exec.Command("zenity", "--file-selection", "--directory", "--title=Select ArteCraft Apps Folder", "--filename="+current+"/")
		cmd.Env = cleanHostEnv()
		if out, err := cmd.Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		cmd := exec.Command("kdialog", "--getexistingdirectory", current)
		cmd.Env = cleanHostEnv()
		if out, err := cmd.Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func createPlatformShortcuts(meta AppMeta, inst InstalledApp, desktop, startMenu bool) error {
	home, _ := os.UserHomeDir()
	icon := findAppIconPNG(inst.InstallDir)
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=ArteCraft — Free Native %s Alternative
Exec="%s"
Icon=%s
Terminal=false
Categories=Graphics;Office;AudioVideo;Development;
StartupNotify=true
`, meta.Name, meta.Replaces, inst.ExecPath, icon)

	if startMenu {
		appsDir := filepath.Join(home, ".local", "share", "applications")
		_ = os.MkdirAll(appsDir, 0755)
		_ = os.WriteFile(filepath.Join(appsDir, "artecraft-"+meta.Slug+".desktop"), []byte(entry), 0755)
	}
	if desktop {
		for _, deskName := range []string{"Desktop", "Рабочий стол"} { // i18n-ignore: имя папки на диске, а не текст интерфейса
			deskDir := filepath.Join(home, deskName)
			if st, err := os.Stat(deskDir); err == nil && st.IsDir() {
				_ = os.WriteFile(filepath.Join(deskDir, "artecraft-"+meta.Slug+".desktop"), []byte(entry), 0755)
			}
		}
	}
	return nil
}

func createSelfDesktopShortcut() error {
	exe := resolveSelfLinuxExec()
	if exe == "" {
		return fmt.Errorf("executable not resolved")
	}
	home, _ := os.UserHomeDir()
	iconPath := filepath.Join(getAppDataDir(), "artecraft.png")
	if b, rerr := embeddedUI.ReadFile("ui/logo.png"); rerr == nil {
		_ = os.WriteFile(iconPath, b, 0644)
	} else if b2, rerr2 := embeddedUI.ReadFile("ui/favicon.png"); rerr2 == nil {
		_ = os.WriteFile(iconPath, b2, 0644)
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ArteCraft
Comment=13 Free Native Rust Creative, Office & CAD Apps
Exec="%s"
Icon=%s
Terminal=false
Categories=Utility;Graphics;Office;
StartupNotify=true
`, exe, iconPath)
	appsDir := filepath.Join(home, ".local", "share", "applications")
	_ = os.MkdirAll(appsDir, 0755)
	_ = os.WriteFile(filepath.Join(appsDir, "artecraft-launcher.desktop"), []byte(entry), 0755)
	for _, deskName := range []string{"Desktop", "Рабочий стол"} { // i18n-ignore: имя папки на диске, а не текст интерфейса
		deskDir := filepath.Join(home, deskName)
		if st, serr := os.Stat(deskDir); serr == nil && st.IsDir() {
			_ = os.WriteFile(filepath.Join(deskDir, "ArteCraft.desktop"), []byte(entry), 0755)
		}
	}
	return nil
}

func setPlatformAutoStart(enable bool) error {
	// Вся логика — в autostart_*.go: запись проверяется чтением назад, а
	// автозапуск ставится с флагом --tray (сразу в трей, без окна).
	return applyAutoStart(enable)
}
