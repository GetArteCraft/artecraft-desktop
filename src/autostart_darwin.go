//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// autostart_darwin.go — LaunchAgent: ~/Library/LaunchAgents/com.artecraft.launcher.plist

func autoStartPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.artecraft.launcher.plist")
}

func autoStartCommandLine() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\"%s\" --tray", exe)
}

// legacyAutoStartPlistPath — старое имя метки (com.getartecraft.launcher),
// от него нужно избавляться, иначе автозапуск остаётся даже после выключения.
func legacyAutoStartPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.getartecraft.launcher.plist")
}

func applyAutoStart(enable bool) error {
	if legacy := legacyAutoStartPlistPath(); legacy != autoStartPlistPath() {
		_ = exec.Command("launchctl", "unload", legacy).Run()
		_ = os.Remove(legacy)
	}
	path := autoStartPlistPath()
	if !enable {
		_ = exec.Command("launchctl", "unload", path).Run()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.artecraft.launcher</string>
  <key>ProgramArguments</key><array><string>%s</string><string>--tray</string></array>
  <key>RunAtLoad</key><true/>
</dict></plist>
`, exe)
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "load", path).Run()
	if !autoStartEnabled() {
		return fmt.Errorf("autostart: plist was not created")
	}
	return nil
}

func autoStartEnabled() bool {
	raw, err := os.ReadFile(autoStartPlistPath())
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "com.artecraft.launcher")
}

func traySupported() bool { return false }
