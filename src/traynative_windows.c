// traynative_windows.c — иконка в системном трее Windows (Shell_NotifyIcon)
// и перехват закрытия окна, чтобы лаунчер сворачивался в трей, а не выходил.
// Только Win32, без внешних зависимостей.
//
// Важно (исправлено в 2.5.3): окно-приёмник сообщений трея НЕ должно быть
// message-only (HWND_MESSAGE). Оболочка Windows не доставляет уведомления
// Shell_NotifyIcon таким окнам — иконка просто не появляется. Делаем обычное
// скрытое popup-окно: сообщения приходят, иконка видна, меню работает.
#include "traynative.h"

static HWND     g_target = NULL;      /* окно WebView2 (показывать/прятать) */
static HICON    g_icon = NULL;
static WNDPROC  g_prev = NULL;        /* старый WndProc окна WebView2 */
static int      g_subclassed = 0;
static wchar_t  g_tip[128] = L"ArteCraft";
static UINT     g_taskbar_created = 0;

void artecraft_set_target(HWND hwnd) { g_target = hwnd; }
HWND artecraft_target(void) { return g_target; }

static HICON artecraft_icon(void) {
    if (g_icon == NULL) {
        /* Иконка лежит в ресурсе exe (RT_GROUP_ICON id 1, см. rsrc_windows_amd64.syso). */
        g_icon = (HICON)LoadImageW(GetModuleHandleW(NULL), MAKEINTRESOURCEW(1),
                                   IMAGE_ICON, 0, 0, LR_DEFAULTSIZE | LR_SHARED);
        if (g_icon == NULL) {
            g_icon = LoadIconW(NULL, (LPCWSTR)IDI_APPLICATION);
        }
    }
    return g_icon;
}

static void artecraft_fill(NOTIFYICONDATAW *nid, HWND hwnd) {
    ZeroMemory(nid, sizeof(*nid));
    nid->cbSize = sizeof(*nid);
    nid->hWnd = hwnd;
    nid->uID = 1;
}

int artecraft_tray_add(HWND hwnd, const wchar_t *tip) {
    if (hwnd == NULL) return 0;
    if (tip != NULL) lstrcpynW(g_tip, tip, 127);
    NOTIFYICONDATAW nid;
    artecraft_fill(&nid, hwnd);
    nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
    nid.uCallbackMessage = ARTECRAFT_TRAY_MSG;
    nid.hIcon = artecraft_icon();
    lstrcpynW(nid.szTip, g_tip, 127);
    BOOL ok = Shell_NotifyIconW(NIM_ADD, &nid);
    if (!ok) {
        /* оболочка могла ещё не подняться (быстрый старт после логина) — пробуем ещё раз */
        Sleep(300);
        ok = Shell_NotifyIconW(NIM_ADD, &nid);
    }
    return (int)ok;
}

void artecraft_tray_tip(HWND hwnd, const wchar_t *tip) {
    if (hwnd == NULL || tip == NULL) return;
    lstrcpynW(g_tip, tip, 127);
    NOTIFYICONDATAW nid;
    artecraft_fill(&nid, hwnd);
    nid.uFlags = NIF_TIP;
    lstrcpynW(nid.szTip, g_tip, 127);
    Shell_NotifyIconW(NIM_MODIFY, &nid);
}

void artecraft_tray_remove(HWND hwnd) {
    if (hwnd == NULL) return;
    NOTIFYICONDATAW nid;
    artecraft_fill(&nid, hwnd);
    Shell_NotifyIconW(NIM_DELETE, &nid);
}

/* Если окно принадлежит другому процессу (режим Edge/Chrome --app),
   ищем его по заголовку — иначе вернуть окно из трея было бы нечем. */
static HWND artecraft_resolve_target(void) {
    if (g_target) return g_target;
    HWND h = FindWindowW(NULL, L"ArteCraft");
    if (h) g_target = h;
    return h;
}

void artecraft_show_window(void) {
    HWND h = artecraft_resolve_target();
    if (!h) return;
    ShowWindow(h, SW_SHOW);
    ShowWindow(h, SW_RESTORE);
    SetForegroundWindow(h);
}

void artecraft_hide_window(void) {
    HWND h = artecraft_resolve_target();
    if (h) ShowWindow(h, SW_HIDE);
}

void artecraft_menu(HWND hwnd, const wchar_t *openLabel, const wchar_t *hideLabel, const wchar_t *quitLabel) {
    HMENU m = CreatePopupMenu();
    if (!m) return;
    AppendMenuW(m, MF_STRING, ARTECRAFT_ID_OPEN, openLabel);
    AppendMenuW(m, MF_STRING, ARTECRAFT_ID_HIDE, hideLabel);
    AppendMenuW(m, MF_SEPARATOR, 0, NULL);
    AppendMenuW(m, MF_STRING, ARTECRAFT_ID_QUIT, quitLabel);
    POINT pt;
    GetCursorPos(&pt);
    SetForegroundWindow(hwnd);
    TrackPopupMenu(m, TPM_RIGHTBUTTON | TPM_BOTTOMALIGN, pt.x, pt.y, 0, hwnd, NULL);
    PostMessageW(hwnd, WM_NULL, 0, 0);
    DestroyMenu(m);
}

/* Скрытое обычное popup-окно для сообщений трея (НЕ message-only). */
HWND artecraft_create_window(void) {
    static const wchar_t *cls = L"ArteCraftTraySink";
    WNDCLASSEXW wc;
    ZeroMemory(&wc, sizeof(wc));
    wc.cbSize = sizeof(wc);
    wc.lpfnWndProc = artecraftTrayProc;
    wc.hInstance = GetModuleHandleW(NULL);
    wc.lpszClassName = cls;
    RegisterClassExW(&wc);
    HWND hwnd = CreateWindowExW(0, cls, L"ArteCraft", WS_POPUP, 0, 0, 0, 0,
                                NULL, NULL, GetModuleHandleW(NULL), NULL);
    if (g_taskbar_created == 0) {
        g_taskbar_created = RegisterWindowMessageW(L"TaskbarCreated");
    }
    return hwnd;
}

UINT artecraft_taskbar_created(void) { return g_taskbar_created; }

void artecraft_message_loop(void) {
    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
}

void artecraft_post_quit(HWND hwnd) {
    if (hwnd) PostMessageW(hwnd, WM_CLOSE, 0, 0);
}

void artecraft_subclass(HWND hwnd) {
    if (!hwnd || g_subclassed) return;
    g_prev = (WNDPROC)SetWindowLongPtrW(hwnd, GWLP_WNDPROC, (LONG_PTR)artecraftSubclassProc);
    g_subclassed = 1;
}

void artecraft_unsubclass(HWND hwnd) {
    if (hwnd && g_subclassed && g_prev) {
        SetWindowLongPtrW(hwnd, GWLP_WNDPROC, (LONG_PTR)g_prev);
        g_subclassed = 0;
    }
}

/* Для окна WebView2: отдать сообщение его прежнему обработчику. */
LRESULT artecraft_call_prev(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam) {
    if (!g_prev) return DefWindowProcW(hwnd, msg, wparam, lparam);
    return CallWindowProcW(g_prev, hwnd, msg, wparam, lparam);
}

/* Для окна-приёмника трея: у него нет «прежнего» обработчика — только DefWindowProc. */
LRESULT artecraft_default_proc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam) {
    return DefWindowProcW(hwnd, msg, wparam, lparam);
}

void artecraft_destroy_window(HWND hwnd) {
    if (hwnd) DestroyWindow(hwnd);
}

/* Показать нативное сообщение об ошибке (без консоли и без браузера). */
void artecraft_message_box(const wchar_t *title, const wchar_t *text) {
    MessageBoxW(NULL, text, title, MB_ICONWARNING | MB_OK | MB_SETFOREGROUND);
}
