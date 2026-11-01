//go:build darwin && !cgo

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func runPlatformDesktopUI(localURL string) {
	home, _ := os.UserHomeDir()
	relBins := []string{
		"Google Chrome.app/Contents/MacOS/Google Chrome",
		"Brave Browser.app/Contents/MacOS/Brave Browser",
		"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"Chromium.app/Contents/MacOS/Chromium",
		"Vivaldi.app/Contents/MacOS/Vivaldi",
		"Opera.app/Contents/MacOS/Opera",
	}
	var chromePaths []string
	for _, r := range relBins {
		chromePaths = append(chromePaths, filepath.Join("/Applications", r))
		if home != "" {
			chromePaths = append(chromePaths, filepath.Join(home, "Applications", r))
		}
	}
	for _, bin := range chromePaths {
		if _, err := os.Stat(bin); err == nil {
			profileDir := filepath.Join(getAppDataDir(), "AppWindowProfile")
			_ = os.MkdirAll(profileDir, 0755)
			cmd := exec.Command(bin,
				"--app="+localURL,
				"--window-size=1280,820",
				"--user-data-dir="+profileDir,
				"--no-first-run",
				"--no-default-browser-check",
			)
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
	_ = exec.Command("open", localURL).Start()
	waitBrowserSessionOrSignal()
}

func launchInstalledBinary(inst InstalledApp) error {
	if strings.HasSuffix(strings.ToLower(inst.ExecPath), ".app") {
		_ = exec.Command("xattr", "-cr", inst.ExecPath).Run()
		return exec.Command("open", inst.ExecPath).Start()
	}
	cmd := exec.Command(inst.ExecPath)
	cmd.Dir = inst.InstallDir
	return cmd.Start()
}

func openExternalURL(u string) error {
	return exec.Command("open", u).Start()
}

func openFolderNative(dir string) error {
	_ = os.MkdirAll(dir, 0755)
	return exec.Command("open", dir).Start()
}

func pickFolderNative(current string) string {
	_ = os.MkdirAll(current, 0755)
	script := fmt.Sprintf(`POSIX path of (choose folder with prompt "Select ArteCraft Apps Folder" default location POSIX file %q)`, current)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func createPlatformShortcuts(meta AppMeta, inst InstalledApp, desktop, startMenu bool) error {
	home, _ := os.UserHomeDir()
	if startMenu {
		userApps := filepath.Join(home, "Applications")
		_ = os.MkdirAll(userApps, 0755)
		dst := filepath.Join(userApps, filepath.Base(inst.ExecPath))
		if dst != inst.ExecPath {
			_ = os.Remove(dst)
			_ = os.Symlink(inst.ExecPath, dst)
		}
	}
	if desktop {
		desk := filepath.Join(home, "Desktop", filepath.Base(inst.ExecPath))
		_ = os.Remove(desk)
		_ = os.Symlink(inst.ExecPath, desk)
	}
	return nil
}

func createSelfDesktopShortcut() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if realExe, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = realExe
	}
	home, _ := os.UserHomeDir()
	target := exe
	if idx := strings.Index(exe, ".app/Contents/MacOS/"); idx != -1 {
		target = exe[:idx+4]
	}
	desk := filepath.Join(home, "Desktop", filepath.Base(target))
	_ = os.Remove(desk)
	return os.Symlink(target, desk)
}

func setPlatformAutoStart(enable bool) error {
	// Вся логика — в autostart_*.go: запись проверяется чтением назад, а
	// автозапуск ставится с флагом --tray (сразу в трей, без окна).
	return applyAutoStart(enable)
}
