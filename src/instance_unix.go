//go:build !windows

package main

// instance_unix.go — межпроцессная блокировка на Unix (Linux, macOS).
// Держим flock на lock-файле: ядро снимает его само, если процесс упал,
// поэтому «зависших» блокировок после падения не остаётся.

import (
	"os"
	"syscall"
)

var instanceLockFile *os.File

func acquireOSInstanceLock() bool {
	_ = os.MkdirAll(getAppDataDir(), 0755)
	f, err := os.OpenFile(instanceLockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return true // нет доступа к файлу — не мешаем запуску
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return false
	}
	instanceLockFile = f
	return true
}

func releaseOSInstanceLock() {
	if instanceLockFile != nil {
		_ = syscall.Flock(int(instanceLockFile.Fd()), syscall.LOCK_UN)
		_ = instanceLockFile.Close()
		instanceLockFile = nil
	}
}

// activateExistingWindowFallback на Unix ничего не делает: окно поднимает
// локальный HTTP-эндпоинт /api/app/show.
func activateExistingWindowFallback() bool { return false }
