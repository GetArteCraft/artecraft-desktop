//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// autostart_linux.go — XDG-автозапуск: ~/.config/autostart/artecraft.desktop.
// Exec указывает на текущий бинарь (или на AppImage, если запущены из него)
// с флагом --tray, чтобы при входе в сессию лаунчер жил в трее.

func autoStartDesktopPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "autostart", "artecraft.desktop")
}

func autoStartCommandLine() string {
	exe := resolveSelfLinuxExec()
	if strings.TrimSpace(exe) == "" {
		return ""
	}
	return fmt.Sprintf("\"%s\" --tray", exe)
}

func applyAutoStart(enable bool) error {
	path := autoStartDesktopPath()
	if !enable {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	cmdline := autoStartCommandLine()
	if cmdline == "" {
		return fmt.Errorf("autostart: cannot resolve binary path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	body := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=ArteCraft\n" +
		"Comment=ArteCraft launcher (tray)\n" +
		"Exec=" + cmdline + "\n" +
		"Icon=artecraft\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n" +
		"Categories=Utility;\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return err
	}
	if !autoStartEnabled() {
		return fmt.Errorf("autostart: desktop file was not created")
	}
	return nil
}

func autoStartEnabled() bool {
	raw, err := os.ReadFile(autoStartDesktopPath())
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "ArteCraft")
}

// traySupported: на Linux трей зависит от окружения, поэтому кнопка «свернуть
// в трей» показывается, но закрытие окна завершает программу.
func traySupported() bool { return false }

var _ = exec.Command
