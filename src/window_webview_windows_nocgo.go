//go:build windows && !cgo

package main

// Без cgo нативное окно WebView2 недоступно — используется запасной путь
// (Edge/Chrome в режиме приложения, см. platform_windows.go).
func runNativeWebViewWindow(string) bool { return false }
