//go:build linux && !cgo

package main

func tryNativeLinuxWebView(localURL string) bool {
	return false
}
