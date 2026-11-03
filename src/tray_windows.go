//go:build windows && cgo

package main

/*
#cgo LDFLAGS: -luser32 -lshell32 -lgdi32
#include <stdlib.h>
#include "traynative.h"
*/
import "C"

import (
	"runtime"
	"sync"
	"unicode/utf16"
	"unsafe"
)

// tray_windows.go — сворачивание в системный трей.
//
// Раньше галочка «сворачивать в трей» только писалась в state.json и ничего не
// делала: не было ни иконки, ни перехвата закрытия окна. Теперь:
//   • иконка ArteCraft живёт в трее, пока приложение работает;
//   • клик по иконке (двойной) или пункт «Открыть» показывает окно;
//   • крестик окна прячет его в трей вместо выхода (если галочка включена);
//   • пункт «Выход» завершает приложение по-настоящему.
//
// Реализация на чистом Win32: Shell_NotifyIcon + перехват WndProc окна
// WebView2. Никаких сторонних библиотек, поэтому cgo-сборка не тяжелеет.

// startHiddenInTray выставляется флагом --tray (автозапуск): окно создаётся,
// но сразу прячется, чтобы при входе в систему не выскакивало поверх всех.
var startHiddenInTray bool

var (
	trayMu      sync.Mutex
	trayHWND    C.HWND
	trayOnQuit  func()
	trayActive  bool
	hideOnClose int32 // читается из C-колбэка без блокировок
)

// trayOpenCh получает сигнал «показать окно» из меню трея. Нужен в режиме
// Edge/Chrome --app: окно принадлежит другому процессу, и после его закрытия
// лаунчер остаётся в трее, а по «Открыть» запускает окно снова.
var trayOpenCh = make(chan struct{}, 4)

func notifyTrayOpen() {
	select {
	case trayOpenCh <- struct{}{}:
	default:
	}
	showMainWindow()
}

// trayLabels — подписи меню трея на языке интерфейса (15 языков, как в i18n.js).
// i18n-ignore-start: строки трея живут в нативном меню Win32, до JS-словаря не
// доходят, поэтому таблица продублирована здесь — по одному набору на язык.
type trayLabelSet struct{ tip, open, hide, quit string }

var trayLabelsByLang = map[string]trayLabelSet{
	"en": {"ArteCraft — running in the background", "Open ArteCraft", "Minimize to tray", "Quit"},                    // i18n-ignore
	"de": {"ArteCraft — läuft im Hintergrund", "ArteCraft öffnen", "In den Tray minimieren", "Beenden"},              // i18n-ignore
	"fr": {"ArteCraft — en arrière-plan", "Ouvrir ArteCraft", "Réduire dans la zone de notification", "Quitter"},     // i18n-ignore
	"es": {"ArteCraft — en segundo plano", "Abrir ArteCraft", "Minimizar a la bandeja", "Salir"},                     // i18n-ignore
	"pt": {"ArteCraft — em segundo plano", "Abrir o ArteCraft", "Minimizar para a bandeja", "Sair"},                  // i18n-ignore
	"it": {"ArteCraft — in background", "Apri ArteCraft", "Riduci nella barra", "Esci"},                              // i18n-ignore
	"nl": {"ArteCraft — actief op de achtergrond", "ArteCraft openen", "Naar systeemvak minimaliseren", "Afsluiten"}, // i18n-ignore
	"pl": {"ArteCraft — działa w tle", "Otwórz ArteCraft", "Minimalizuj do zasobnika", "Zakończ"},                    // i18n-ignore
	"ru": {"ArteCraft — работает в фоне", "Открыть ArteCraft", "Свернуть в трей", "Выход"},                           // i18n-ignore
	"ja": {"ArteCraft — バックグラウンドで実行中", "ArteCraft を開く", "トレイに最小化", "終了"},                                             // i18n-ignore
	"ko": {"ArteCraft — 백그라운드 실행 중", "ArteCraft 열기", "트레이로 최소화", "종료"},                                               // i18n-ignore
	"zh": {"ArteCraft — 正在后台运行", "打开 ArteCraft", "最小化到托盘", "退出"},                                                     // i18n-ignore
	"tr": {"ArteCraft — arka planda çalışıyor", "ArteCraft'ı aç", "Tepsiye küçült", "Çıkış"},                         // i18n-ignore
	"ar": {"ArteCraft — يعمل في الخلفية", "فتح ArteCraft", "تصغير إلى شريط النظام", "خروج"},                          // i18n-ignore
	"hi": {"ArteCraft — बैकग्राउंड में चल रहा है", "ArteCraft खोलें", "ट्रे में छोटा करें", "बंद करें"},              // i18n-ignore
} // i18n-ignore-end

func trayLabels() trayLabelSet {
	mu.Lock()
	lang := appState.Locale
	mu.Unlock()
	if set, ok := trayLabelsByLang[lang]; ok {
		return set
	}
	return trayLabelsByLang["en"]
}

// winWide выделяет UTF-16 строку в куче C. Освобождать не обязательно:
// подписи трея создаются один раз за запуск и нужны до самого выхода.
func winWide(s string) *C.wchar_t {
	units := utf16.Encode([]rune(s))
	buf := C.malloc(C.size_t((len(units) + 1) * 2))
	if buf == nil {
		return nil
	}
	ptr := (*[1 << 20]uint16)(buf)
	for i, u := range units {
		ptr[i] = u
	}
	ptr[len(units)] = 0
	return (*C.wchar_t)(buf)
}

// installTray запускает иконку в трее и перехват закрытия окна.
// onQuit вызывается при выборе «Выход» в меню.
func installTray(window unsafe.Pointer, hideOnCloseEnabled bool, onQuit func()) bool {
	// window == nil допустимо: в режиме Edge/Chrome --app собственного окна нет,
	// трей всё равно нужен (показ/скрытие ищутся по заголовку).
	trayMu.Lock()
	if trayActive {
		trayMu.Unlock()
		return true
	}
	trayActive = true
	trayOnQuit = onQuit
	if hideOnCloseEnabled {
		hideOnClose = 1
	}
	trayMu.Unlock()

	if window != nil {
		hwndTarget := C.HWND(window)
		C.artecraft_set_target(hwndTarget)
		if hideOnCloseEnabled {
			C.artecraft_subclass(hwndTarget)
		}
	}

	labels := trayLabels()
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd := C.artecraft_create_window()
		if hwnd == nil {
			return
		}
		trayMu.Lock()
		trayHWND = hwnd
		trayMu.Unlock()
		if C.artecraft_tray_add(hwnd, winWide(labels.tip)) == 0 {
			logLine("Shell_NotifyIcon(NIM_ADD) failed — tray icon not created")
		} else {
			logLine("tray icon added")
		}
		C.artecraft_message_loop()
		C.artecraft_tray_remove(hwnd)
		C.artecraft_destroy_window(hwnd)
	}()
	return true
}

// setTrayHideOnClose включает/выключает перехват крестика на ходу
// (пользователь может менять галочку в настройках без перезапуска).
func setTrayHideOnClose(enable bool) {
	if enable {
		hideOnClose = 1
	} else {
		hideOnClose = 0
	}
}

// showMainWindow / hideMainWindow — вызываются из UI-ручек.
func showMainWindow() { C.artecraft_show_window() }
func hideMainWindow() { C.artecraft_hide_window() }

//export artecraftTrayProc
func artecraftTrayProc(hwnd C.HWND, msg C.UINT, wparam C.WPARAM, lparam C.LPARAM) C.LRESULT {
	switch uint32(msg) {
	case uint32(C.ARTECRAFT_TRAY_MSG):
		switch uint32(lparam) & 0xFFFF {
		case C.WM_LBUTTONDBLCLK, C.WM_LBUTTONUP:
			notifyTrayOpen()
		case C.WM_RBUTTONUP:
			labels := trayLabels()
			C.artecraft_menu(hwnd, winWide(labels.open), winWide(labels.hide), winWide(labels.quit))
		}
		return 0
	case C.WM_COMMAND:
		switch uint32(wparam) & 0xFFFF {
		case C.ARTECRAFT_ID_OPEN:
			notifyTrayOpen()
		case C.ARTECRAFT_ID_HIDE:
			C.artecraft_hide_window()
		case C.ARTECRAFT_ID_QUIT:
			trayMu.Lock()
			cb := trayOnQuit
			trayMu.Unlock()
			if cb != nil {
				cb()
			}
		}
		return 0
	case C.WM_CLOSE:
		C.artecraft_post_quit(hwnd)
		return 0
	}
	// Окно-приёмник трея не имеет «прежнего» обработчика: раньше здесь
	// вызывался CallWindowProc окна WebView2 с чужим HWND — из-за этого
	// сообщения уходили не туда и окно могло зависнуть.
	if uint32(msg) == uint32(C.artecraft_taskbar_created()) && msg != 0 {
		// Explorer перезапустился — иконку надо добавить заново.
		trayMu.Lock()
		h := trayHWND
		trayMu.Unlock()
		if h != nil {
			C.artecraft_tray_add(h, winWide(trayLabels().tip))
			logLine("tray icon re-added after Explorer restart")
		}
		return 0
	}
	return C.artecraft_default_proc(hwnd, msg, wparam, lparam)
}

//export artecraftSubclassProc
func artecraftSubclassProc(hwnd C.HWND, msg C.UINT, wparam C.WPARAM, lparam C.LPARAM) C.LRESULT {
	switch uint32(msg) {
	case C.WM_CLOSE:
		if hideOnClose == 1 {
			C.artecraft_hide_window()
			return 0
		}
	case C.WM_SYSCOMMAND:
		cmd := uint32(wparam) & 0xFFF0
		if hideOnClose == 1 && (cmd == C.SC_MINIMIZE || cmd == C.SC_CLOSE) {
			C.artecraft_hide_window()
			return 0
		}
	}
	return C.artecraft_call_prev(hwnd, msg, wparam, lparam)
}

// removeTray убирает иконку при выходе.
func removeTray() {
	trayMu.Lock()
	hwnd := trayHWND
	trayHWND = nil
	trayActive = false
	trayMu.Unlock()
	if hwnd != nil {
		C.artecraft_post_quit(hwnd)
	}
	C.artecraft_unsubclass(C.artecraft_target())
}
