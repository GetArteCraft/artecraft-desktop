//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdlib.h>

typedef int gboolean;
typedef void* gpointer;
typedef unsigned long gulong;
typedef void (*GCallback)(void);

static void (*fn_gtk_main_quit)(void) = NULL;

static void on_gtk_destroy(gpointer widget, gpointer data) {
    if (fn_gtk_main_quit) {
        fn_gtk_main_quit();
    }
}

static int run_dlopen_webkit_gtk(const char *url) {
    // Load WebKit2GTK first (tries ABI 4.1 on Ubuntu 24.04/Fedora/Arch, then 4.0 on Ubuntu 20.04/22.04/Debian)
    void *wk = dlopen("libwebkit2gtk-4.1.so.0", RTLD_LAZY | RTLD_GLOBAL);
    if (!wk) {
        wk = dlopen("libwebkit2gtk-4.0.so.37", RTLD_LAZY | RTLD_GLOBAL);
    }
    if (!wk) {
        return 0;
    }

    void *gtk = dlopen("libgtk-3.so.0", RTLD_LAZY | RTLD_GLOBAL);
    void *gobj = dlopen("libgobject-2.0.so.0", RTLD_LAZY | RTLD_GLOBAL);
    if (!gtk || !gobj) {
        return 0;
    }

    gboolean (*gtk_init_check)(int*, char***) = dlsym(gtk, "gtk_init_check");
    gpointer (*gtk_window_new)(int) = dlsym(gtk, "gtk_window_new");
    void (*gtk_window_set_title)(gpointer, const char*) = dlsym(gtk, "gtk_window_set_title");
    void (*gtk_window_set_default_size)(gpointer, int, int) = dlsym(gtk, "gtk_window_set_default_size");
    void (*gtk_window_set_position)(gpointer, int) = dlsym(gtk, "gtk_window_set_position");
    void (*gtk_widget_set_size_request)(gpointer, int, int) = dlsym(gtk, "gtk_widget_set_size_request");
    void (*gtk_container_add)(gpointer, gpointer) = dlsym(gtk, "gtk_container_add");
    void (*gtk_widget_show_all)(gpointer) = dlsym(gtk, "gtk_widget_show_all");
    void (*gtk_main)(void) = dlsym(gtk, "gtk_main");
    fn_gtk_main_quit = dlsym(gtk, "gtk_main_quit");
    gpointer (*gtk_settings_get_default)(void) = dlsym(gtk, "gtk_settings_get_default");

    gpointer (*webkit_web_view_new)(void) = dlsym(wk, "webkit_web_view_new");
    void (*webkit_web_view_load_uri)(gpointer, const char*) = dlsym(wk, "webkit_web_view_load_uri");

    gulong (*g_signal_connect_data)(gpointer, const char*, GCallback, gpointer, gpointer, int) = dlsym(gobj, "g_signal_connect_data");
    void (*g_object_set)(gpointer, const char*, ...) = dlsym(gobj, "g_object_set");

    if (!gtk_init_check || !gtk_window_new || !gtk_window_set_title ||
        !gtk_window_set_default_size || !gtk_container_add || !gtk_widget_show_all ||
        !gtk_main || !fn_gtk_main_quit || !webkit_web_view_new || !webkit_web_view_load_uri ||
        !g_signal_connect_data) {
        return 0;
    }

    int argc = 0;
    char **argv = NULL;
    if (!gtk_init_check(&argc, &argv)) {
        return 0;
    }

    if (gtk_settings_get_default && g_object_set) {
        gpointer settings = gtk_settings_get_default();
        if (settings) {
            g_object_set(settings, "gtk-application-prefer-dark-theme", 1, NULL);
        }
    }

    gpointer win = gtk_window_new(0); // GTK_WINDOW_TOPLEVEL = 0
    gtk_window_set_title(win, "ArteCraft");
    gtk_window_set_default_size(win, 1280, 820);
    if (gtk_widget_set_size_request) {
        gtk_widget_set_size_request(win, 960, 620);
    }
    if (gtk_window_set_position) {
        gtk_window_set_position(win, 1); // GTK_WIN_POS_CENTER = 1
    }

    gpointer webview = webkit_web_view_new();
    gtk_container_add(win, webview);
    g_signal_connect_data(win, "destroy", (GCallback)on_gtk_destroy, NULL, NULL, 0);

    webkit_web_view_load_uri(webview, url);
    gtk_widget_show_all(win);
    gtk_main();
    return 1;
}
*/
import "C"
import "unsafe"

func tryNativeLinuxWebView(localURL string) bool {
	cs := C.CString(localURL)
	defer C.free(unsafe.Pointer(cs))
	return C.run_dlopen_webkit_gtk(cs) == 1
}
