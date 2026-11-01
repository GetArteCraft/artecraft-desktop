package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRepoURL(t *testing.T) {
	cases := map[string]string{
		"obsproject/obs-studio":                        "obsproject/obs-studio",
		"https://github.com/obsproject/obs-studio":     "obsproject/obs-studio",
		"https://github.com/obsproject/obs-studio.git": "obsproject/obs-studio",
		"https://github.com/Kanaries/Rath/tree/main":   "Kanaries/Rath",
		"github.com/blender/blender":                   "blender/blender",
	}
	for in, want := range cases {
		got, err := NormalizeRepoURL(in)
		if err != nil || got != want {
			t.Fatalf("%q → %q (%v), ожидали %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "https://gitlab.com/a/b", "just-one-word", "ftp://github.com/a/b"} {
		if _, err := NormalizeRepoURL(bad); err == nil {
			t.Fatalf("ожидали ошибку для %q", bad)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	got := splitArgs(`--profile "my profile" -v 'x y'`)
	want := []string{"--profile", "my profile", "-v", "x y"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("splitArgs: %q, ожидали %q", got, want)
	}
	if len(splitArgs("   ")) != 0 {
		t.Fatal("пустая строка должна давать пустой список")
	}
}

func TestAssetClassification(t *testing.T) {
	cases := []struct{ name, os, kind string }{
		{"App-1.0-windows-x64-portable.zip", "windows", "zip"},
		{"App-1.0-macOS-universal.dmg", "macos", "dmg"},
		{"App-1.0-linux-x86_64.tar.gz", "linux", "targz"},
		{"App-1.0-x86_64.AppImage", "linux", "appimage"},
	}
	for _, c := range cases {
		if got := assetOS(c.name); got != c.os {
			t.Fatalf("%s: os=%q, ожидали %q", c.name, got, c.os)
		}
		if got := assetKind(c.name); got != c.kind {
			t.Fatalf("%s: kind=%q, ожидали %q", c.name, got, c.kind)
		}
	}
}

func TestScorePreview(t *testing.T) {
	strong := &GithubPreview{Stars: 40000, License: "MIT", PushedAt: "2099-01-01T00:00:00Z", ReleaseTag: "v1"}
	if s := ScorePreview(strong); s < 80 {
		t.Fatalf("сильный репозиторий должен иметь высокий балл, получили %d", s)
	}
	weak := &GithubPreview{Stars: 3, Archived: true, PushedAt: "2001-01-01T00:00:00Z"}
	if s := ScorePreview(weak); s > 20 {
		t.Fatalf("слабый репозиторий должен иметь низкий балл, получили %d", s)
	}
}

func TestIsGithubDownloadURL(t *testing.T) {
	ok := []string{
		"https://github.com/owner/repo/releases/download/v1/app.zip",
		"https://objects.githubusercontent.com/x/y",
	}
	bad := []string{"http://github.com/a/b", "https://evil.com/github.com/a/b", "file:///etc/passwd"}
	for _, u := range ok {
		if !isGithubDownloadURL(u) {
			t.Fatalf("должен разрешать %q", u)
		}
	}
	for _, u := range bad {
		if isGithubDownloadURL(u) {
			t.Fatalf("должен запрещать %q", u)
		}
	}
}

// makeTestDeb собирает минимальный .deb (ar + data.tar.gz) с одним бинарником.
func makeTestDeb(t *testing.T, dir string) string {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho hi\n")
	if err := tw.WriteHeader(&tar.Header{Name: "./usr/bin/testapp", Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()

	var ar bytes.Buffer
	ar.WriteString("!<arch>\n")
	writeMember := func(name string, payload []byte) {
		hdr := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", name, "0", "0", "0", "100644", len(payload))
		ar.WriteString(hdr)
		ar.Write(payload)
		if len(payload)%2 == 1 {
			ar.WriteByte('\n')
		}
	}
	writeMember("debian-binary", []byte("2.0\n"))
	writeMember("control.tar.gz", []byte{})
	writeMember("data.tar.gz", data.Bytes())

	p := filepath.Join(dir, "test.deb")
	if err := os.WriteFile(p, ar.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractDeb(t *testing.T) {
	tmp := t.TempDir()
	deb := makeTestDeb(t, tmp)
	dest := filepath.Join(tmp, "app")
	_ = os.MkdirAll(dest, 0755)
	exe, err := extractDeb(deb, dest)
	if err != nil {
		t.Fatalf("extractDeb: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(exe), "usr/bin/testapp") {
		t.Fatalf("неожиданный путь: %s", exe)
	}
	if st, err := os.Stat(exe); err != nil || st.Mode()&0111 == 0 {
		t.Fatalf("файл не исполняемый: %v", err)
	}
}
