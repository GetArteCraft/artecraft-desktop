//go:build !(windows && cgo)

package main

import "unsafe"

// tray_other.go — заглушки для платформ без нативного трея (Linux, macOS) и
// для сборки Windows без cgo. Поведение честное: в UI показывается, что трей
// недоступен, а настройка не делает вид, что включилась.

// startHiddenInTray выставляется флагом --tray (автозапуск).
var startHiddenInTray bool

func installTray(window unsafe.Pointer, hideOnCloseEnabled bool, onQuit func()) bool { return false }
func setTrayHideOnClose(enable bool)                                                 {}
func showMainWindow()                                                                {}
func hideMainWindow()                                                                {}
func removeTray()                                                                    {}
