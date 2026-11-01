//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

@interface ArteCraftAppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate, WKUIDelegate, WKNavigationDelegate>
@property (strong, nonatomic) NSWindow *window;
@property (strong, nonatomic) WKWebView *webView;
@property (strong, nonatomic) NSString *startURL;
@end

@implementation ArteCraftAppDelegate

- (void)setupMenus {
    NSMenu *menubar = [[NSMenu alloc] init];

    // 1. Application Menu (Cmd+H, Cmd+Q)
    NSMenuItem *appMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:appMenuItem];
    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"ArteCraft"];
    [appMenu addItemWithTitle:@"Hide ArteCraft" action:@selector(hide:) keyEquivalent:@"h"];
    [appMenu addItem:[NSMenuItem separatorItem]];
    [appMenu addItemWithTitle:@"Quit ArteCraft" action:@selector(terminate:) keyEquivalent:@"q"];
    [appMenuItem setSubmenu:appMenu];

    // 2. Edit Menu (CRITICAL on macOS so Cmd+C, Cmd+V, Cmd+X, Cmd+A, Cmd+Z work inside WKWebView inputs)
    NSMenuItem *editMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:editMenuItem];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [editMenu addItemWithTitle:@"Undo" action:NSSelectorFromString(@"undo:") keyEquivalent:@"z"];
    NSMenuItem *redoItem = [[NSMenuItem alloc] initWithTitle:@"Redo" action:NSSelectorFromString(@"redo:") keyEquivalent:@"Z"];
    [editMenu addItem:redoItem];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
    [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    [editMenuItem setSubmenu:editMenu];

    // 3. Window Menu (Cmd+M, Cmd+W)
    NSMenuItem *winMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:winMenuItem];
    NSMenu *winMenu = [[NSMenu alloc] initWithTitle:@"Window"];
    [winMenu addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
    [winMenu addItemWithTitle:@"Close" action:@selector(performClose:) keyEquivalent:@"w"];
    [winMenuItem setSubmenu:winMenu];

    [NSApp setMainMenu:menubar];
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [self setupMenus];

    NSRect frame = NSMakeRect(0, 0, 1280, 820);
    NSUInteger style = NSWindowStyleMaskTitled |
                       NSWindowStyleMaskClosable |
                       NSWindowStyleMaskMiniaturizable |
                       NSWindowStyleMaskResizable;
    self.window = [[NSWindow alloc] initWithContentRect:frame
                                              styleMask:style
                                                backing:NSBackingStoreBuffered
                                                  defer:NO];
    [self.window setTitle:@"ArteCraft"];
    [self.window setMinSize:NSMakeSize(960, 640)];
    if (@available(macOS 10.14, *)) {
        [self.window setAppearance:[NSAppearance appearanceNamed:NSAppearanceNameDarkAqua]];
    }
    [self.window setBackgroundColor:[NSColor colorWithSRGBRed:0.024 green:0.031 blue:0.059 alpha:1.0]];
    [self.window center];
    [self.window setDelegate:self];

    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    self.webView = [[WKWebView alloc] initWithFrame:[[self.window contentView] bounds] configuration:cfg];
    [self.webView setAutoresizingMask:(NSViewWidthSizable | NSViewHeightSizable)];
    [self.webView setUIDelegate:self];
    [self.webView setNavigationDelegate:self];
    if ([self.webView respondsToSelector:@selector(setValue:forKey:)]) {
        [self.webView setValue:@(NO) forKey:@"drawsBackground"];
    }
    [[self.window contentView] addSubview:self.webView];

    NSURL *nsurl = [NSURL URLWithString:self.startURL];
    NSURLRequest *req = [NSURLRequest requestWithURL:nsurl cachePolicy:NSURLRequestReloadIgnoringLocalCacheData timeoutInterval:30.0];
    [self.webView loadRequest:req];

    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

// Open target="_blank" external links in default macOS browser
- (WKWebView *)webView:(WKWebView *)webView
createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
   forNavigationAction:(WKNavigationAction *)navigationAction
        windowFeatures:(WKWindowFeatures *)windowFeatures {
    if (!navigationAction.targetFrame.isMainFrame && navigationAction.request.URL != nil) {
        [[NSWorkspace sharedWorkspace] openURL:navigationAction.request.URL];
    }
    return nil;
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}
@end

static void runCocoaWebView(const char *cUrl) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];
        ArteCraftAppDelegate *del = [[ArteCraftAppDelegate alloc] init];
        del.startURL = [NSString stringWithUTF8String:cUrl];
        [app setDelegate:del];
        [app run];
    }
}
*/
import "C"
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"
)

func runPlatformDesktopUI(localURL string) {
	cs := C.CString(localURL)
	defer C.free(unsafe.Pointer(cs))
	C.runCocoaWebView(cs)
}

func launchInstalledBinary(inst InstalledApp) error {
	if strings.HasSuffix(strings.ToLower(inst.ExecPath), ".app") {
		_ = exec.Command("xattr", "-cr", inst.ExecPath).Run()
		return exec.Command("open", inst.ExecPath).Start()
	}
	cmd := exec.Command(inst.ExecPath)
	cmd.Dir = inst.InstallDir
	return cmd.Start()
}

func openExternalURL(u string) error {
	return exec.Command("open", u).Start()
}

func openFolderNative(dir string) error {
	_ = os.MkdirAll(dir, 0755)
	return exec.Command("open", dir).Start()
}

func pickFolderNative(current string) string {
	_ = os.MkdirAll(current, 0755)
	script := fmt.Sprintf(`POSIX path of (choose folder with prompt "Select ArteCraft Apps Folder" default location POSIX file %q)`, current)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func createPlatformShortcuts(meta AppMeta, inst InstalledApp, desktop, startMenu bool) error {
	home, _ := os.UserHomeDir()
	if startMenu {
		userApps := filepath.Join(home, "Applications")
		_ = os.MkdirAll(userApps, 0755)
		dst := filepath.Join(userApps, filepath.Base(inst.ExecPath))
		if dst != inst.ExecPath {
			_ = os.Remove(dst)
			_ = os.Symlink(inst.ExecPath, dst)
		}
	}
	if desktop {
		desk := filepath.Join(home, "Desktop", filepath.Base(inst.ExecPath))
		_ = os.Remove(desk)
		_ = os.Symlink(inst.ExecPath, desk)
	}
	return nil
}

func resolveSelfMacTarget() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if realExe, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = realExe
	}
	if idx := strings.Index(exe, ".app/Contents/MacOS/"); idx != -1 {
		return exe[:idx+4]
	}
	return exe
}

func createSelfDesktopShortcut() error {
	target := resolveSelfMacTarget()
	if target == "" {
		return fmt.Errorf("executable not resolved")
	}
	home, _ := os.UserHomeDir()
	desk := filepath.Join(home, "Desktop", filepath.Base(target))
	_ = os.Remove(desk)
	return os.Symlink(target, desk)
}

func setPlatformAutoStart(enable bool) error {
	// Вся логика — в autostart_*.go: запись проверяется чтением назад, а
	// автозапуск ставится с флагом --tray (сразу в трей, без окна).
	return applyAutoStart(enable)
}
