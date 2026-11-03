//go:build windows && cgo

package main

/*
#cgo LDFLAGS: -luser32
#include <stdlib.h>
#include "traynative.h"
*/
import "C"

import (
	"os"
	"path/filepath"
	"time"

	webview "github.com/jchv/go-webview2"
)

var _ = C.artecraft_message_box

// runNativeWebViewWindow открывает настоящее окно Windows с WebView2 —
// без браузера, без адресной строки и вкладок. Иконка окна берётся из
// ресурса .exe (RT_GROUP_ICON id 1, см. rsrc_windows_amd64.syso).
// Возвращает false, если WebView2 Runtime в системе не найден.
func newWebViewWindow(dataDir string) webview.WebView {
	var w webview.WebView
	func() {
		defer func() {
			if r := recover(); r != nil {
				logLine("webview2 creation panic: %v", r)
				w = nil
			}
		}()
		w = webview.NewWithOptions(webview.WebViewOptions{
			DataPath:  dataDir,
			AutoFocus: true,
			WindowOptions: webview.WindowOptions{
				Title:  "ArteCraft",
				Width:  1280,
				Height: 820,
				IconId: 1,
				Center: true,
			},
		})
	}()
	return w
}

// runNativeWebViewWindow открывает настоящее окно Windows с WebView2.
// Если профиль WebView2 оказался битым (частая причина пустого окна
// «ArteCraft (не отвечает)»), пробуем чистый каталог данных, а в самом конце
// показываем нативное сообщение. Браузер не открывается никогда.
func runNativeWebViewWindow(localURL string) bool {
	dirs := []string{
		filepath.Join(getAppDataDir(), "WebView2Data"),
		filepath.Join(getAppDataDir(), "WebView2Data-clean"),
		filepath.Join(os.TempDir(), "ArteCraftWebView2"),
	}
	var w webview.WebView
	for i, dir := range dirs {
		_ = os.MkdirAll(dir, 0755)
		w = newWebViewWindow(dir)
		if w != nil {
			logLine("webview2 window created with data dir %s (attempt %d)", dir, i+1)
			break
		}
		logLine("webview2 attempt %d failed for %s", i+1, dir)
	}
	if w == nil {
		logLine("webview2 runtime unavailable — showing message box")
		C.artecraft_message_box(winWide("ArteCraft"),
			winWide("ArteCraft needs the Microsoft Edge WebView2 Runtime.\n\n"+
				"Install it once from microsoft.com/edge/webview2 and start ArteCraft again.\n"+
				"Details: %LOCALAPPDATA%\\ArteCraft\\launcher.log"))
		return false
	}
	defer w.Destroy()
	w.Navigate(localURL)

	hwnd := w.Window()
	mu.Lock()
	closeToTray := appState.CloseToTray
	mu.Unlock()

	terminate := func() {
		w.Terminate()
		time.AfterFunc(1500*time.Millisecond, func() {
			_ = os.Remove(activeInstanceFilePath())
			releaseOSInstanceLock()
			os.Exit(0)
		})
	}

	mainWindowMu.Lock()
	mainWindow = hwnd
	mainTerminate = terminate
	mainWindowMu.Unlock()

	// Иконку в трее ставим ВСЕГДА: она должна быть, даже если крестик настроен
	// закрывать программу. Галочка «сворачивать в трей» решает только, что
	// делает крестик. Раньше трей создавался лишь при включённой галочке — если
	// настройка сбивалась, иконки не было вовсе и вернуть окно было нечем.
	if hwnd != nil {
		if installTray(hwnd, closeToTray, terminate) {
			logLine("tray installed (hideOnClose=%v hidden=%v)", closeToTray, startHiddenInTray)
			if startHiddenInTray {
				w.Dispatch(func() { hideMainWindow() })
			}
		} else {
			logLine("tray install returned false")
		}
		defer removeTray()
	} else {
		logLine("window handle is nil, tray not installed")
	}

	w.Run()
	return true
}
