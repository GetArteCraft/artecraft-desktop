package main

// instance_lock.go — «покажи уже открытое окно» для повторного запуска.
//
// Межпроцессную блокировку держит ОС (см. instance_windows.go / instance_unix.go):
// именованный мьютекс на Windows и flock на Unix. Здесь только поиск работающего
// экземпляра и просьба показать окно. Браузер не участвует никогда.

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// instanceLockPath — путь lock-файла (используется на Unix для flock).
func instanceLockPath() string {
	return fmt.Sprintf("%s%clauncher.lock", getAppDataDir(), os.PathSeparator)
}

// runningInstanceURL возвращает адрес уже работающего экземпляра, если он жив.
func runningInstanceURL() (string, bool) {
	raw, err := os.ReadFile(activeInstanceFilePath())
	if err != nil {
		return "", false
	}
	prevURL := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(prevURL, "http://127.0.0.1:") {
		return "", false
	}
	cl := &http.Client{Timeout: 700 * time.Millisecond}
	resp, gerr := cl.Get(prevURL + "/api/status")
	if gerr != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	return prevURL, true
}

// activateRunningInstance просит существующий экземпляр показать окно.
func activateRunningInstance(baseURL string) bool {
	cl := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := cl.Post(baseURL+"/api/app/show", "application/json", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// focusRunningInstance ждёт появления живого экземпляра (первый может ещё
// стартовать), затем поднимает его окно. Возвращает true, если окно найдено.
func focusRunningInstance(wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if prevURL, alive := runningInstanceURL(); alive {
			if activateRunningInstance(prevURL) {
				return true
			}
			if activateExistingWindowFallback() {
				return true
			}
			return false
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	return activateExistingWindowFallback()
}
