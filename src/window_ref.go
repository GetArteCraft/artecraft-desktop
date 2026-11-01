package main

import (
	"sync"
	"unsafe"
)

// window_ref.go — ссылка на главное окно, чтобы фичами окна (трей, показать/
// скрыть) можно было управлять из HTTP-ручек уже после старта окна.
var (
	mainWindowMu  sync.Mutex
	mainWindow    unsafe.Pointer
	mainTerminate func()
)
