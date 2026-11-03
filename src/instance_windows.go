//go:build windows

package main

// instance_windows.go — единственный экземпляр на Windows.
//
// Используем именованный мьютекс ядра: он либо создаётся нами, либо уже
// существует (значит лаунчер запущен). Никаких эвристик по PID и lock-файлам —
// именно из-за них в 2.5.1 второй запуск мог «отобрать» блокировку у первого,
// пока тот ещё стартовал, и открыть второе окно WebView2 на том же каталоге
// данных (отсюда белое окно «ArteCraft (Не отвечает)»).

import (
	"syscall"
	"unsafe"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW  = kernel32.NewProc("CreateMutexW")
	procFindWindowW   = user32.NewProc("FindWindowW")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
	procIsIconic      = user32.NewProc("IsIconic")
	instanceMutex     syscall.Handle
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
	swShow             = 5
)

func acquireOSInstanceLock() bool {
	name, err := syscall.UTF16PtrFromString(`Local\ArteCraftLauncher-SingleInstance`)
	if err != nil {
		return true
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		// не смогли создать мьютекс — не блокируем работу приложения
		return true
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		_ = syscall.CloseHandle(syscall.Handle(h))
		return false
	}
	instanceMutex = syscall.Handle(h)
	return true
}

func releaseOSInstanceLock() {
	if instanceMutex != 0 {
		_ = syscall.CloseHandle(instanceMutex)
		instanceMutex = 0
	}
}

// activateExistingWindowFallback поднимает окно уже запущенного лаунчера
// средствами Win32 — на случай, если его локальный HTTP-сервер ещё не поднялся
// или ответил слишком поздно.
func activateExistingWindowFallback() bool {
	title, err := syscall.UTF16PtrFromString("ArteCraft")
	if err != nil {
		return false
	}
	cls, _ := syscall.UTF16PtrFromString("")
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return false
	}
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	} else {
		_, _, _ = procShowWindow.Call(hwnd, swShow)
	}
	_, _, _ = procSetForeground.Call(hwnd)
	return true
}
