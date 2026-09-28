package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.10", "v0.1.9", true},
		{"v0.2.0", "v0.1.99", true},
		{"v0.1.9", "v0.1.9", false},
		{"v0.1.8", "v0.1.9", false},
		{"v0.1.10", "dev", false},
		{"", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestAssetNameFor(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "sysbox-darwin-arm64"},
		{"darwin", "amd64", "sysbox-darwin-amd64"},
		{"linux", "amd64", "sysbox-linux-amd64"},
		{"linux", "arm64", "sysbox-linux-arm64"},
		{"windows", "amd64", "sysbox-windows-amd64.exe"},
		{"windows", "arm64", "sysbox-windows-arm64.exe"},
	}
	for _, c := range cases {
		if got := AssetNameFor(c.goos, c.goarch); got != c.want {
			t.Errorf("AssetNameFor(%q, %q) = %q，期望 %q", c.goos, c.goarch, got, c.want)
		}
	}
	want := AssetNameFor(runtime.GOOS, runtime.GOARCH)
	if got := AssetName(); got != want {
		t.Errorf("AssetName() = %q，期望 %q", got, want)
	}
}

// fakeGitHub 模拟 releases/latest 与附件下载，校验下载请求携带了 octet-stream 与令牌
func fakeGitHub(t *testing.T, binary []byte, sum string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.1.5","assets":[
			{"name":%q,"url":"%s/assets/1","size":%d},
			{"name":"checksums.txt","url":"%s/assets/2","size":10}]}`,
			AssetName(), srv.URL, len(binary), srv.URL)
	})
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/octet-stream" || r.Header.Get("Authorization") != "Bearer tk" {
			http.Error(w, "bad request headers", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/assets/1" {
			_, _ = w.Write(binary)
			return
		}
		fmt.Fprintf(w, "%s  %s\n", sum, AssetName())
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestInstall(t *testing.T) {
	binary := []byte("#!/bin/sh\necho new\n")
	h := sha256.Sum256(binary)
	srv := fakeGitHub(t, binary, hex.EncodeToString(h[:]))
	c := &Client{Repo: "o/r", APIBase: srv.URL, Token: "tk", HTTP: srv.Client()}

	exe := filepath.Join(t.TempDir(), "sysbox")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	if err := c.Install(context.Background(), rel, exe, func(done, _ int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(binary) || last != int64(len(binary)) {
		t.Errorf("替换结果有误：%q，进度 %d", got, last)
	}
	// Windows 没有可执行权限位，只在 Unix 上检查
	if fi, _ := os.Stat(exe); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o755 {
		t.Errorf("权限应为 755，实际 %v", fi.Mode().Perm())
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	srv := fakeGitHub(t, []byte("tampered"), "0000000000000000000000000000000000000000000000000000000000000000")
	c := &Client{Repo: "o/r", APIBase: srv.URL, Token: "tk", HTTP: srv.Client()}
	exe := filepath.Join(t.TempDir(), "sysbox")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, _ := c.Latest(context.Background())
	if err := c.Install(context.Background(), rel, exe, nil); err == nil {
		t.Fatal("校验失败时应报错")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Error("校验失败时不应替换当前版本")
	}
}
