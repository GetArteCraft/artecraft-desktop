package main

// launcher_log.go — простой файловый лог лаунчера.
//
// Зачем: на Windows всё, что пишется в stderr, при сборке с -H windowsgui
// никуда не видно. Без лога невозможно понять, почему не появилась иконка
// в трее или не поднялось окно. Пишем коротко, с ротацией по 512 КБ.
// Посмотреть путь: пуск с флагом --diagnose (печатает состояние и путь к логу).

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var logMu sync.Mutex

func logPath() string {
	return filepath.Join(getAppDataDir(), "launcher.log")
}

func logLine(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	_ = os.MkdirAll(getAppDataDir(), 0755)
	path := logPath()
	if st, err := os.Stat(path); err == nil && st.Size() > 512*1024 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
