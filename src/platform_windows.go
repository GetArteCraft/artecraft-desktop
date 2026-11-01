//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func runPlatformDesktopUI(localURL string) {
	// 1) Настоящее окно Windows с WebView2 — если рантайм установлен.
	if runNativeWebViewWindow(localURL) {
		return
	}
	// 2) Запасной путь: Edge/Chrome в режиме приложения (без адресной строки и вкладок).
	edgePaths := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
	}
	for _, bin := range edgePaths {
		if _, err := os.Stat(bin); err == nil {
			profileDir := filepath.Join(getAppDataDir(), "AppWindowProfile")
			_ = os.MkdirAll(profileDir, 0755)
			cmd := exec.Command(bin, "--app="+localURL, "--window-size=1280,820", "--user-data-dir="+profileDir,
				"--no-first-run", "--no-default-browser-check", "--disable-features=msEdgeFirstRunExperience,Translate",
				"--disable-sync", "--no-service-autorun", "--app-auto-launched")
			if cmd.Start() == nil {
				_ = cmd.Wait()
				return
			}
		}
	}
	_ = openExternalURL(localURL)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

func launchInstalledBinary(inst InstalledApp) error {
	cmd := exec.Command(inst.ExecPath)
	cmd.Dir = inst.InstallDir
	return cmd.Start()
}

func openExternalURL(u string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

func openFolderNative(dir string) error {
	_ = os.MkdirAll(dir, 0755)
	return exec.Command("explorer.exe", dir).Start()
}

func pickFolderNative(current string) string {
	return ""
}

// hiddenCmd запускает процесс без окна консоли. Без HideWindow + CREATE_NO_WINDOW
// (0x08000000) powershell на долю секунды показывает чёрное окно — именно это
// мигало в панели задач при создании ярлыков и проверке настроек.
func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

func createPlatformShortcuts(meta AppMeta, inst InstalledApp, desktop, startMenu bool) error {
	if !desktop && !startMenu {
		return nil
	}
	// Раньше ярлык создавался только на рабочем столе, флаг «меню Пуск» игнорировался.
	name := escapePS(meta.Name)
	parts := make([]string, 0, 2)
	if desktop {
		parts = append(parts, fmt.Sprintf(
			"$l=$s.CreateShortcut((Join-Path ([Environment]::GetFolderPath('Desktop')) '%s.lnk'));$l.TargetPath='%s';$l.WorkingDirectory='%s';$l.Save()",
			name, escapePS(inst.ExecPath), escapePS(inst.InstallDir)))
	}
	if startMenu {
		parts = append(parts, fmt.Sprintf(
			"$sm=Join-Path ([Environment]::GetFolderPath('Programs')) 'ArteCraft';New-Item -ItemType Directory -Force -Path $sm|Out-Null;"+
				"$l=$s.CreateShortcut((Join-Path $sm '%s.lnk'));$l.TargetPath='%s';$l.WorkingDirectory='%s';$l.Save()",
			name, escapePS(inst.ExecPath), escapePS(inst.InstallDir)))
	}
	ps := "$s=(New-Object -COM WScript.Shell);" + strings.Join(parts, ";")
	return hiddenCmd("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", ps).Run()
}

// escapePS — одинарные кавычки внутри одинарных кавычек PowerShell удваиваются.
func escapePS(s string) string { return strings.ReplaceAll(s, "'", "''") }

func createSelfDesktopShortcut() error {
	return nil
}

// setPlatformAutoStart оставлен для совместимости: реализация — в
// autostart_windows.go (ключ HKCU\...\Run). Раньше здесь была заглушка
// «return nil», из-за неё автозапуск «включался» в настройках и не работал.
func setPlatformAutoStart(enable bool) error {
	return applyAutoStart(enable)
}
