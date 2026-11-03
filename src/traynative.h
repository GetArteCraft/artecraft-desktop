// traynative.h — объявления нативного слоя трея (Windows).
// Реализация в traynative.c. Заголовок общий для cgo-файла tray_windows.go.
#ifndef ARTECRAFT_TRAYNATIVE_H
#define ARTECRAFT_TRAYNATIVE_H

#include <windows.h>
#include <shellapi.h>
#include <wchar.h>

#define ARTECRAFT_TRAY_MSG (WM_APP + 42)
#define ARTECRAFT_ID_OPEN  1001
#define ARTECRAFT_ID_HIDE  1002
#define ARTECRAFT_ID_QUIT  1003

/* Колбэки, реализованные на Go (экспортируются через //export). */
LRESULT CALLBACK artecraftTrayProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
LRESULT CALLBACK artecraftSubclassProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);

void   artecraft_set_target(HWND hwnd);
HWND   artecraft_target(void);
int    artecraft_tray_add(HWND hwnd, const wchar_t *tip);
void   artecraft_tray_remove(HWND hwnd);
void   artecraft_tray_tip(HWND hwnd, const wchar_t *tip);
void   artecraft_show_window(void);
void   artecraft_hide_window(void);
void   artecraft_menu(HWND hwnd, const wchar_t *openLabel, const wchar_t *hideLabel, const wchar_t *quitLabel);
HWND   artecraft_create_window(void);
void   artecraft_message_loop(void);
void   artecraft_post_quit(HWND hwnd);
void   artecraft_subclass(HWND hwnd);
void   artecraft_unsubclass(HWND hwnd);
LRESULT artecraft_call_prev(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
LRESULT artecraft_default_proc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
UINT    artecraft_taskbar_created(void);
void    artecraft_message_box(const wchar_t *title, const wchar_t *text);
void   artecraft_destroy_window(HWND hwnd);

#endif
