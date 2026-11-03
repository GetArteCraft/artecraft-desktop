//go:build windows

package main

// platform_windows.go — как лаунчер показывает интерфейс на Windows.
//
// Порядок (2.5.4):
//   1) если установлен Microsoft Edge WebView2 Runtime — нативное окно WebView2;
//   2) иначе (или если окно не поднялось) — Edge/Chrome/любой Chromium в режиме
//      приложения (--app): отдельное окно без адресной строки и вкладок;
//   3) браузера «вообще» и второго окна больше не бывает: перед стартом окна
//      рантайм проверяется, а неудачная попытка WebView2 окно за собой не оставляет.
//
// Иконка трея создаётся в нашем процессе в любом режиме, поэтому «свернуть в трей»
// работает и с окном Chromium: по трею окно показывается/прячется по заголовку,
// а если пользователь закрыл окно при включённом «сворачивать в трей» — лаунчер
// остаётся жить в трее и по «Открыть» поднимает окно заново.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func runPlatformDesktopUI(localURL string) {
	if webView2Available() {
		if runNativeWebViewWindow(localURL) {
			return
		}
		logLine("native WebView2 window failed, switching to app window")
	} else {
		logLine("WebView2 runtime not installed, using app window")
	}
	runAppWindowLoop(localURL)
}

// chromiumCandidates — установленные Chromium-браузеры, в порядке предпочтения.
func chromiumCandidates() []string {
	env := func(k string) string { return os.Getenv(k) }
	var out []string
	add := func(parts ...string) {
		p := filepath.Join(parts...)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	add(env("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe")
	add(env("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe")
	add(env("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe")
	add(env("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe")
	add(env("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe")
	add(env("ProgramFiles"), "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
	add(env("ProgramFiles"), "Vivaldi", "Application", "vivaldi.exe")
	add(env("LOCALAPPDATA"), "Programs", "Opera", "opera.exe")
	return out
}

func startAppWindow(localURL string) *exec.Cmd {
	profileDir := filepath.Join(getAppDataDir(), "AppWindowProfile")
	_ = os.MkdirAll(profileDir, 0755)
	for _, bin := range chromiumCandidates() {
		cmd := exec.Command(bin,
			"--app="+localURL,
			"--window-size=1280,880",
			"--user-data-dir="+profileDir,
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-features=msEdgeFirstRunExperience,Translate,dialMediaRouteProvider",
			"--disable-sync",
			"--no-service-autorun",
		)
		if err := cmd.Start(); err == nil {
			logLine("app window started: %s", bin)
			return cmd
		}
		logLine("cannot start %s", bin)
	}
	return nil
}

func runAppWindowLoop(localURL string) {
	// Трей живёт, пока жив лаунчер — независимо от того, чьё окно показывает UI.
	if !installTray(nil, true, func() {
		logLine("quit from tray menu")
		mu.Lock()
		saveStateLocked()
		mu.Unlock()
		_ = os.Remove(activeInstanceFilePath())
		releaseOSInstanceLock()
		os.Exit(0)
	}) {
		logLine("tray not available in app-window mode")
	}
	defer removeTray()

	for {
		cmd := startAppWindow(localURL)
		if cmd == nil {
			showNativeMessage("ArteCraft",
				"ArteCraft needs Microsoft Edge WebView2 Runtime or any Chromium browser "+
					"(Edge, Chrome, Brave) to show its window.\n\n"+
					"Install WebView2 once from microsoft.com/edge/webview2 and start ArteCraft again.")
			return
		}
		_ = cmd.Wait()

		mu.Lock()
		closeToTray := appState.CloseToTray
		mu.Unlock()
		if !closeToTray {
			logLine("app window closed, closeToTray is off — exiting")
			return
		}
		logLine("app window closed, staying in tray (closeToTray on)")
		// Ждём «Открыть» из трея. Таймаут — страховка от вечного висяка.
		select {
		case <-trayOpenCh:
			logLine("reopening window from tray")
			continue
		case <-time.After(12 * time.Hour):
			return
		}
	}
}

// showNativeMessage — одно окно-сообщение Windows (MessageBoxW), без консоли
// и без пустого окна WebView2. Используется только когда UI показать нечем.
func showNativeMessage(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	const mbIconWarning, mbSetForeground = 0x00000030, 0x00010000
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)),
		uintptr(mbIconWarning|mbSetForeground|0x00000000))
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

// pickFolderNative — системный диалог выбора папки. До 2.5.4 здесь стояла
// заглушка, возвращавшая "" — кнопка «Выбрать папку» в настройках молча ничего не
// делала. Диалог показываем через .NET FolderBrowserDialog в отдельном процессе
// PowerShell (окно консоли скрыто), поэтому cgo и COM в лаунчере не нужны.
func pickFolderNative(current string) string {
	if current == "" {
		current, _ = os.UserHomeDir()
	}
	_ = os.MkdirAll(current, 0755)
	script := "Add-Type -AssemblyName System.Windows.Forms | Out-Null; " +
		"$d = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$d.Description = 'Select ArteCraft apps folder'; " +
		"$d.ShowNewFolderButton = $true; " +
		"$d.SelectedPath = '" + escapePS(current) + "'; " +
		"if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { " +
		"[Console]::Out.Write($d.SelectedPath) }"
	cmd := hiddenCmd("powershell", "-NoProfile", "-NonInteractive", "-STA",
		"-WindowStyle", "Hidden", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		logLine("pick folder failed: %v", err)
		return ""
	}
	picked := strings.TrimSpace(string(out))
	if picked == "" {
		logLine("pick folder cancelled")
		return ""
	}
	return picked
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
