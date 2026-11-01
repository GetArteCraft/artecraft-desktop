//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

// autostart_windows.go — автозапуск через HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//
// Ключ пишется на текущего пользователя (без прав администратора), значение —
// путь к exe в кавычках плюс флаг --tray, чтобы при входе в систему лаунчер
// поднимался сразу в трей, а не окном поверх всех.
//
// Важно: реестр читается и пишется нативно (golang.org/x/sys/windows/registry),
// а не через reg.exe. Раньше каждая проверка автозапуска запускала процесс
// reg.exe — UI опрашивает /api/status каждые 1.5 с, и в панели задач каждые пару
// секунд мигало чёрное окошко консоли. Теперь процессов нет вообще.
// После записи значение читается обратно: настройка считается применённой
// только если строка в реестре действительно есть.

const autoStartRegPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const autoStartValueName = "ArteCraft"

// кэш состояния автозапуска: реестр читается не чаще раза в 30 секунд,
// чтобы опрос /api/status не дёргал диск на каждый кадр.
var (
	autoStartMu     sync.Mutex
	autoStartCache  bool
	autoStartCached bool
	autoStartAt     time.Time
)

func invalidateAutoStartCache() {
	autoStartMu.Lock()
	autoStartCached = false
	autoStartMu.Unlock()
}

func autoStartCommandLine() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	exe, _ = filepath.Abs(exe)
	// Кавычки обязательны: в пути бывают пробелы («C:\Program Files\...»).
	return fmt.Sprintf("\"%s\" --tray", exe)
}

func applyAutoStart(enable bool) error {
	defer invalidateAutoStartCache()
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open run key: %v", err)
	}
	defer func() { _ = k.Close() }()

	if !enable {
		if err := k.DeleteValue(autoStartValueName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("autostart: delete value: %v", err)
		}
		if autoStartEnabled() {
			return fmt.Errorf("autostart: registry value was not removed")
		}
		return nil
	}

	cmdline := autoStartCommandLine()
	if cmdline == "" {
		return fmt.Errorf("autostart: cannot resolve exe path")
	}
	if err := k.SetStringValue(autoStartValueName, cmdline); err != nil {
		return fmt.Errorf("autostart: set value: %v", err)
	}
	if !autoStartEnabled() {
		return fmt.Errorf("autostart: registry value was not written")
	}
	return nil
}

// autoStartEnabled читает реестр и говорит, включён ли автозапуск на самом деле —
// это то, что показывается в настройках, а не просто флаг в state.json.
func autoStartEnabled() bool {
	autoStartMu.Lock()
	if autoStartCached && time.Since(autoStartAt) < 30*time.Second {
		v := autoStartCache
		autoStartMu.Unlock()
		return v
	}
	autoStartMu.Unlock()

	ok := false
	if k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegPath, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue(autoStartValueName); err == nil {
			ok = strings.TrimSpace(v) != ""
		}
		_ = k.Close()
	}

	autoStartMu.Lock()
	autoStartCache = ok
	autoStartCached = true
	autoStartAt = time.Now()
	autoStartMu.Unlock()
	return ok
}

// traySupported — на Windows трей есть всегда.
func traySupported() bool { return true }
