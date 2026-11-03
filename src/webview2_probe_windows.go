//go:build windows

package main

// webview2_probe_windows.go — проверка, установлен ли Microsoft Edge WebView2 Runtime.
//
// Зачем: go-webview2 создаёт окно ДО того, как выясняется, что рантайма нет.
// В результате на машине без WebView2 оставалось белое окно «ArteCraft (не отвечает)»,
// а рядом открывалось окно браузера — два окна, ровно как жаловался владелец.
// Теперь рантайм проверяется заранее: нет рантайма — сразу идём в оконный режим Edge.

import (
	"golang.org/x/sys/windows/registry"
)

func webView2Available() bool {
	// Официальные ключи Edge WebView2 (per-machine и per-user).
	paths := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`},
	}
	for _, p := range paths {
		k, err := registry.OpenKey(p.root, p.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		ver, _, verr := k.GetStringValue("pv")
		_ = k.Close()
		if verr == nil && len(ver) > 1 && ver != "0.0.0.0" {
			logLine("WebView2 runtime found: %s", ver)
			return true
		}
	}
	logLine("WebView2 runtime NOT found in registry")
	return false
}
