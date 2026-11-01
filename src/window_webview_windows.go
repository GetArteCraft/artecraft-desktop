//go:build windows && cgo

package main

import (
	"os"
	"path/filepath"
	"time"

	webview "github.com/jchv/go-webview2"
)

// runNativeWebViewWindow открывает настоящее окно Windows с WebView2 —
// без браузера, без адресной строки и вкладок. Иконка окна берётся из
// ресурса .exe (RT_GROUP_ICON id 1, см. rsrc_windows_amd64.syso).
// Возвращает false, если WebView2 Runtime в системе не найден.
func runNativeWebViewWindow(localURL string) bool {
	var w webview.WebView
	func() {
		defer func() {
			if r := recover(); r != nil {
				w = nil
			}
		}()
		w = webview.NewWithOptions(webview.WebViewOptions{
			DataPath:  filepath.Join(getAppDataDir(), "WebView2Data"),
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
	if w == nil {
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
		// Даём окну закрыться штатно, потом убираем файл активного инстанса.
		time.AfterFunc(1500*time.Millisecond, func() {
			_ = os.Remove(activeInstanceFilePath())
			os.Exit(0)
		})
	}

	mainWindowMu.Lock()
	mainWindow = hwnd
	mainTerminate = terminate
	mainWindowMu.Unlock()

	// Трей нужен, если пользователь включил «сворачивать в трей» или запуск
	// пришёл из автозапуска с флагом --tray.
	if hwnd != nil && (closeToTray || startHiddenInTray) {
		if installTray(hwnd, closeToTray, terminate) && startHiddenInTray {
			w.Dispatch(func() { hideMainWindow() })
		}
		defer removeTray()
	}

	w.Run()
	return true
}
