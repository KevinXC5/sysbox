package claudeupdate

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testVersion = "2.1.999"

// fakeClaudeEnv 让测试二进制在被当作已下载的 claude 启动时进入假安装逻辑
const fakeClaudeEnv = "SYSBOX_FAKE_CLAUDE"

// TestMain 在测试二进制被当作 claude 重新执行时扮演官方安装子命令。
// 下载下来的文件必须与发布清单的 SHA-256 一致，Windows 又无法执行 shell 脚本，
// 因此测试直接复制自身二进制作为下载内容。
func TestMain(m *testing.M) {
	if os.Getenv(fakeClaudeEnv) == "1" {
		os.Exit(fakeClaude())
	}
	os.Exit(m.Run())
}

// fakeClaude 模拟官方 install 子命令：校验参数，按环境变量失败或睡眠，成功后建立启动链接
func fakeClaude() int {
	args := os.Args[1:]
	if len(args) != 2 || args[0] != "install" || args[1] != testVersion {
		return 20
	}
	if os.Getenv("TEST_EXPECT_DIRECT") != "" && os.Getenv("HTTPS_PROXY") != "" {
		return 19
	}
	if d := os.Getenv("TEST_INSTALL_SLEEP"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 21
		}
		time.Sleep(time.Duration(n) * time.Second)
	}
	if s := os.Getenv("TEST_INSTALL_STATUS"); s != "" && s != "0" {
		code, err := strconv.Atoi(s)
		if err != nil {
			return 21
		}
		return code
	}
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		// 安装环境同时改写了 USERPROFILE，链接落在指定家目录下
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			home = profile
		}
	}
	link := filepath.Join(home, ".local", "bin", launcherName())
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 22
	}
	_ = os.Remove(link)
	if err := os.Symlink(os.Args[0], link); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 23
	}
	return 0
}

// fakeRelease 模拟发布服务，可注入各种网络故障
type fakeRelease struct {
	mu          sync.Mutex
	payload     []byte
	served      []byte // 实际下发的二进制，默认等于 payload
	dropOnce    bool   // 第一次下载发送一半后断开
	ignoreRange bool   // 不支持续传，总是返回完整内容
	failOnce    int    // 第一次下载返回该状态码
	binaryHits  int
	platform    string // 清单与下载路径中的平台键，默认 hostPlatform()
	binary      string // 写入清单的 binary；为空则省略该字段
	file        string // 下载路径上的文件名，默认按平台取 claude 或 claude.exe
}

func (f *fakeRelease) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/latest"), strings.HasSuffix(r.URL.Path, "/stable"):
		fmt.Fprintln(w, testVersion)
	case strings.HasSuffix(r.URL.Path, "/manifest.json"):
		sum := sha256.Sum256(f.payload)
		if f.binary != "" {
			fmt.Fprintf(w, `{"platforms":{%q:{"checksum":%q,"binary":%q}}}`, f.plat(), hex.EncodeToString(sum[:]), f.binary)
		} else {
			fmt.Fprintf(w, `{"platforms":{%q:{"checksum":%q}}}`, f.plat(), hex.EncodeToString(sum[:]))
		}
	case strings.HasSuffix(r.URL.Path, "/"+f.plat()+"/"+f.fileName()):
		f.binaryHits++
		f.serveBinary(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRelease) plat() string {
	if f.platform == "" {
		return hostPlatform()
	}
	return f.platform
}

func (f *fakeRelease) fileName() string {
	if f.file == "" {
		return defaultBinary(f.plat())
	}
	return f.file
}

func (f *fakeRelease) serveBinary(w http.ResponseWriter, r *http.Request) {
	data := f.served
	if data == nil {
		data = f.payload
	}
	if f.failOnce != 0 {
		code := f.failOnce
		f.failOnce = 0
		w.WriteHeader(code)
		return
	}
	if f.dropOnce {
		f.dropOnce = false
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data[:len(data)/2])
		// 劫持连接直接关闭，模拟中途断线
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	}
	start := 0
	if rg := r.Header.Get("Range"); rg != "" && !f.ignoreRange {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		if start >= len(data) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start:])
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

type harness struct {
	t    *testing.T
	home string
	srv  *fakeRelease
	url  string
	logs []string
	opts Options
}

// hostPlatform 测试默认使用的平台键：下载的假安装程序要在本机执行，
// Windows 上必须是带 .exe 的 win32 版本
func hostPlatform() string {
	if runtime.GOOS == "windows" {
		return "win32-x64"
	}
	return "darwin-arm64"
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, home: t.TempDir(), srv: &fakeRelease{payload: fakeClaudeBytes(t)}}
	ts := httptest.NewServer(h.srv)
	t.Cleanup(ts.Close)
	h.url = ts.URL
	h.opts = Options{
		BaseURL: ts.URL, Home: h.home, Platform: hostPlatform(),
		Attempts: 11, RetryDelay: time.Millisecond,
		StallWindow: 5 * time.Second, StallBytes: 1, InstallTimeout: 10 * time.Second,
	}
	return h
}

// fakeClaudeBytes 复制当前测试二进制，作为可在各平台执行的假 claude
func fakeClaudeBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (h *harness) run(target string) (Result, error) {
	// 子进程重新进入 TestMain 时据此扮演假 claude，而不是再跑一遍测试
	h.t.Setenv(fakeClaudeEnv, "1")
	u := New(h.opts, func(e Event) {
		if e.Msg != "" {
			h.logs = append(h.logs, e.Msg)
		}
	})
	return u.Run(context.Background(), target)
}

func (h *harness) log() string { return strings.Join(h.logs, "\n") }

func (h *harness) versionFile() string {
	name := testVersion
	if strings.HasPrefix(h.opts.Platform, "win32-") {
		name += ".exe"
	}
	return filepath.Join(h.home, ".local", "share", "claude", "versions", name)
}

func (h *harness) link() string {
	return filepath.Join(h.home, ".local", "bin", launcherName())
}

// oldInstall 预置一个旧版本并让链接指向它
func (h *harness) oldInstall() string {
	h.t.Helper()
	old := filepath.Join(h.home, ".local", "share", "claude", "versions", "2.1.1")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(h.link()), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.Symlink(old, h.link()); err != nil {
		h.t.Fatal(err)
	}
	return old
}

// assertKeptOld 失败时旧版本链接保持不变
func (h *harness) assertKeptOld(err error, old string) {
	h.t.Helper()
	if err == nil {
		h.t.Fatal("期望失败，实际成功")
	}
	if target, _ := os.Readlink(h.link()); target != old {
		h.t.Errorf("链接被切换到了 %s", target)
	}
}

func TestSuccessThenCached(t *testing.T) {
	h := newHarness(t)
	res, err := h.run("latest")
	if err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if target, _ := os.Readlink(h.link()); target != h.versionFile() || res.Cached {
		t.Fatalf("安装结果有误：link=%s res=%+v", target, res)
	}
	res, err = h.run(testVersion)
	if err != nil || !res.Cached {
		t.Fatalf("第二次应复用本地文件：%+v %v", res, err)
	}
	if h.srv.binaryHits != 1 {
		t.Errorf("二进制只应下载一次，实际 %d 次", h.srv.binaryHits)
	}
}

func TestBadChecksumKeepsOld(t *testing.T) {
	h := newHarness(t)
	old := h.oldInstall()
	h.srv.served = []byte("bad data")
	_, err := h.run("latest")
	h.assertKeptOld(err, old)
	if !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("错误信息应说明校验失败：%v", err)
	}
	if _, statErr := os.Stat(h.versionFile()); !os.IsNotExist(statErr) {
		t.Error("校验失败的文件不应进入版本目录")
	}
}

func TestInstallFailureReported(t *testing.T) {
	h := newHarness(t)
	old := h.oldInstall()
	t.Setenv("TEST_INSTALL_STATUS", "7")
	_, err := h.run("latest")
	h.assertKeptOld(err, old)
	if !strings.Contains(err.Error(), "退出码：7") {
		t.Errorf("应报告退出码：%v", err)
	}
}

func TestInstallTimeout(t *testing.T) {
	h := newHarness(t)
	old := h.oldInstall()
	h.opts.InstallTimeout = 500 * time.Millisecond
	t.Setenv("TEST_INSTALL_SLEEP", "5")
	_, err := h.run("latest")
	h.assertKeptOld(err, old)
	if !strings.Contains(err.Error(), "安装超时") {
		t.Errorf("应报告超时：%v", err)
	}
	if _, statErr := os.Stat(h.versionFile()); statErr != nil {
		t.Error("超时后已下载的文件应保留，供下次复用")
	}
}

func TestResumeAfterDisconnect(t *testing.T) {
	h := newHarness(t)
	h.srv.dropOnce = true
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if !strings.Contains(h.log(), "第 2/11 次尝试") {
		t.Errorf("应在第二次尝试时续传完成：\n%s", h.log())
	}
	got, _ := os.ReadFile(h.versionFile())
	if string(got) != string(h.srv.payload) {
		t.Error("续传后的文件内容不一致")
	}
}

// 服务器不支持续传时，同一次尝试内改为从头下载
func TestRangeIgnoredRestartsInPlace(t *testing.T) {
	h := newHarness(t)
	part := filepath.Join(h.home, ".local", "share", "claude", "update-cache", testVersion+"-"+hostPlatform()+binaryExt(defaultBinary(hostPlatform()))+".part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.srv.ignoreRange = true
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if !strings.Contains(h.log(), "服务器不支持续传") || h.srv.binaryHits != 1 {
		t.Errorf("应在同一次请求内从头下载：hits=%d\n%s", h.srv.binaryHits, h.log())
	}
}

func TestTransientHTTPRetried(t *testing.T) {
	h := newHarness(t)
	h.srv.failOnce = http.StatusServiceUnavailable
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if !strings.Contains(h.log(), "HTTP 503") {
		t.Errorf("应记录 503 并重试：\n%s", h.log())
	}
}

func TestRangeNotSatisfiableRestarts(t *testing.T) {
	h := newHarness(t)
	h.srv.failOnce = http.StatusRequestedRangeNotSatisfiable
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if !strings.Contains(h.log(), "缓存超出远端范围") {
		t.Errorf("应提示从头下载：\n%s", h.log())
	}
}

// 所有尝试都失败时报错，旧版本保持不变
func TestPersistentFailureKeepsOld(t *testing.T) {
	h := newHarness(t)
	old := h.oldInstall()
	h.opts.Attempts = 1
	h.srv.failOnce = http.StatusBadGateway
	_, err := h.run("latest")
	h.assertKeptOld(err, old)
	if !strings.Contains(err.Error(), "已保留续传缓存") {
		t.Errorf("应提示保留了缓存：%v", err)
	}
}

// 直连模式下不继承环境里的代理，安装子命令也拿不到代理变量
func TestDirectIgnoresInheritedProxy(t *testing.T) {
	h := newHarness(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("TEST_EXPECT_DIRECT", "1")
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
}

func TestInvalidTargets(t *testing.T) {
	h := newHarness(t)
	for _, target := range []string{"2.1.186/../../x", "--unknown", "", "1.2"} {
		if _, err := h.run(target); err == nil {
			t.Errorf("非法目标 %q 应被拒绝", target)
		}
	}
	if h.srv.binaryHits != 0 {
		t.Error("非法目标不应发起下载")
	}
}

func TestDryRunStopsBeforeDownload(t *testing.T) {
	h := newHarness(t)
	h.opts.DryRun = true
	res, err := h.run("latest")
	if err != nil || res.Version != testVersion {
		t.Fatalf("演练应解析出版本号：%+v %v", res, err)
	}
	if h.srv.binaryHits != 0 {
		t.Error("演练不应下载二进制")
	}
	if _, statErr := os.Stat(h.versionFile()); !os.IsNotExist(statErr) {
		t.Error("演练不应写入版本目录")
	}
}

func TestResolveProxy(t *testing.T) {
	if p, _ := ResolveProxy("http://127.0.0.1:1", false); p != "" {
		t.Error("代理不可用时应回退为直连")
	}
	if p, _ := ResolveProxy("http://127.0.0.1:7890", true); p != "" {
		t.Error("设置直连时不应使用代理")
	}
}

func TestPlatformID(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "darwin-arm64"},
		{"darwin", "amd64", "darwin-x64"},
		{"windows", "amd64", "win32-x64"},
		{"windows", "arm64", "win32-arm64"},
	}
	for _, c := range cases {
		got, err := platformID(c.goos, c.goarch)
		if err != nil || got != c.want {
			t.Errorf("platformID(%s, %s) = %q, %v，期望 %s", c.goos, c.goarch, got, err, c.want)
		}
	}
	if _, err := platformID("darwin", "386"); err == nil {
		t.Error("不支持的架构应返回错误")
	}
	if _, err := platformID("linux", "amd64"); err == nil {
		t.Error("不支持的系统应返回错误")
	}
}

func TestDefaultBinary(t *testing.T) {
	if defaultBinary("win32-x64") != "claude.exe" || defaultBinary("win32-arm64") != "claude.exe" {
		t.Error("Windows 平台缺省文件名应为 claude.exe")
	}
	if defaultBinary("darwin-arm64") != "claude" || defaultBinary("darwin-x64") != "claude" {
		t.Error("非 Windows 平台缺省文件名应为 claude")
	}
}

// Windows 清单带 binary=claude.exe 时，下载路径、缓存和版本文件都带 .exe，并仍调用 install 子命令
func TestWindowsBinaryName(t *testing.T) {
	h := newHarness(t)
	h.opts.Platform = "win32-x64"
	h.srv.platform = "win32-x64"
	h.srv.binary = "claude.exe"
	h.srv.file = "claude.exe"
	if _, err := h.run("latest"); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if _, err := os.Stat(h.versionFile()); err != nil {
		t.Fatalf("版本文件应为 %s：%v", h.versionFile(), err)
	}
	partDir := filepath.Join(h.home, ".local", "share", "claude", "update-cache")
	entries, err := os.ReadDir(partDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("安装后缓存应被移走：%v %v", entries, err)
	}
	if target, err := os.Readlink(h.link()); err != nil || target != h.versionFile() {
		t.Fatalf("install 子命令收到的路径应为版本文件：%s %v", target, err)
	}
}

// 旧清单没有 binary 字段时，Windows 仍按 claude.exe 下载
func TestWindowsBinaryFallback(t *testing.T) {
	h := newHarness(t)
	h.opts.Platform = "win32-arm64"
	h.srv.platform = "win32-arm64"
	h.srv.file = "claude.exe"
	if _, err := h.run(testVersion); err != nil {
		t.Fatalf("%v\n%s", err, h.log())
	}
	if !strings.HasSuffix(h.versionFile(), ".exe") {
		t.Fatal(h.versionFile())
	}
	if _, err := os.Stat(h.versionFile()); err != nil {
		t.Fatal(err)
	}
}
