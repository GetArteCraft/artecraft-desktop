// traynative.c — иконка в системном трее Windows (Shell_NotifyIcon) и
// перехват закрытия окна, чтобы лаунчер сворачивался в трей, а не выходил.
// Всё на Win32 API без внешних зависимостей — cgo тянет только user32/shell32.
#include "traynative.h"

static HWND    g_target = NULL;
static HICON   g_icon = NULL;
static WNDPROC g_prev = NULL;
static int     g_subclassed = 0;

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

int artecraft_tray_add(HWND hwnd, const wchar_t *tip) {
    NOTIFYICONDATAW nid;
    ZeroMemory(&nid, sizeof(nid));
    nid.cbSize = sizeof(nid);
    nid.hWnd = hwnd;
    nid.uID = 1;
    nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
    nid.uCallbackMessage = ARTECRAFT_TRAY_MSG;
    nid.hIcon = artecraft_icon();
    lstrcpynW(nid.szTip, tip, 127);
    return (int)Shell_NotifyIconW(NIM_ADD, &nid);
}

void artecraft_tray_tip(HWND hwnd, const wchar_t *tip) {
    NOTIFYICONDATAW nid;
    ZeroMemory(&nid, sizeof(nid));
    nid.cbSize = sizeof(nid);
    nid.hWnd = hwnd;
    nid.uID = 1;
    nid.uFlags = NIF_TIP;
    lstrcpynW(nid.szTip, tip, 127);
    Shell_NotifyIconW(NIM_MODIFY, &nid);
}

void artecraft_tray_remove(HWND hwnd) {
    NOTIFYICONDATAW nid;
    ZeroMemory(&nid, sizeof(nid));
    nid.cbSize = sizeof(nid);
    nid.hWnd = hwnd;
    nid.uID = 1;
    Shell_NotifyIconW(NIM_DELETE, &nid);
}

void artecraft_show_window(void) {
    if (!g_target) return;
    ShowWindow(g_target, SW_SHOW);
    ShowWindow(g_target, SW_RESTORE);
    SetForegroundWindow(g_target);
}

void artecraft_hide_window(void) {
    if (g_target) ShowWindow(g_target, SW_HIDE);
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

HWND artecraft_create_window(void) {
    static const wchar_t *cls = L"ArteCraftTraySink";
    WNDCLASSEXW wc;
    ZeroMemory(&wc, sizeof(wc));
    wc.cbSize = sizeof(wc);
    wc.lpfnWndProc = artecraftTrayProc;
    wc.hInstance = GetModuleHandleW(NULL);
    wc.lpszClassName = cls;
    RegisterClassExW(&wc);
    return CreateWindowExW(0, cls, L"ArteCraft", 0, 0, 0, 0, 0,
                           HWND_MESSAGE, NULL, GetModuleHandleW(NULL), NULL);
}

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

LRESULT artecraft_call_prev(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam) {
    if (!g_prev) return DefWindowProcW(hwnd, msg, wparam, lparam);
    return CallWindowProcW(g_prev, hwnd, msg, wparam, lparam);
}

void artecraft_destroy_window(HWND hwnd) {
    if (hwnd) DestroyWindow(hwnd);
}
