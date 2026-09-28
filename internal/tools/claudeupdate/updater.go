// Package claudeupdate 下载并安装指定版本的 Claude Code。
//
// 流程：解析目标版本 → 读取发布清单中的 SHA-256 → 断点续传下载 → 校验 → 调用官方 install 子命令。
// 下载失败时保留缓存，下次运行同一版本会从断点继续；校验失败的文件会被删除且不会安装。
package claudeupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// DefaultBaseURL 官方发布地址
const DefaultBaseURL = "https://downloads.claude.ai/claude-code-releases"

// DefaultProxy 默认代理
const DefaultProxy = "http://127.0.0.1:7890"

var (
	versionRe  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
	checksumRe = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ValidTarget 目标只能是 latest、stable 或合法版本号
func ValidTarget(t string) bool {
	return t == "latest" || t == "stable" || versionRe.MatchString(t)
}

// Stage 更新阶段
type Stage int

const (
	StageResolve  Stage = iota // 解析版本
	StageManifest              // 读取发布清单
	StageDownload              // 下载
	StageInstall               // 调用官方安装
	StageDone
)

// Event 进度事件
type Event struct {
	Stage      Stage
	Msg        string // 日志文本，为空表示只更新进度
	Warn       bool
	Downloaded int64
	Total      int64
	Attempt    int
}

// Options 更新参数
type Options struct {
	BaseURL  string
	Proxy    string // 为空表示直连
	Home     string
	Platform string // 为空时自动检测
	DryRun   bool   // 只解析版本与校验值，不下载也不安装

	Attempts       int           // 下载最多尝试次数
	RetryDelay     time.Duration // 重试间隔
	StallWindow    time.Duration // 低速判定窗口
	StallBytes     int64         // 窗口内至少下载的字节数，否则视为卡住并重试
	InstallTimeout time.Duration
}

// Defaults 默认参数：与原脚本一致，最多尝试 11 次，30 秒内低于 10KB/s 视为卡住
func Defaults(home, proxy string) Options {
	return Options{
		BaseURL:        DefaultBaseURL,
		Proxy:          proxy,
		Home:           home,
		Attempts:       11,
		RetryDelay:     3 * time.Second,
		StallWindow:    30 * time.Second,
		StallBytes:     10 * 1024 * 30,
		InstallTimeout: 60 * time.Second,
	}
}

// Updater 执行一次更新
type Updater struct {
	opt    Options
	client *http.Client
	emit   func(Event)
}

// New 创建更新器；progress 接收进度事件，可为 nil
func New(opt Options, progress func(Event)) *Updater {
	if progress == nil {
		progress = func(Event) {}
	}
	u := &Updater{opt: opt, emit: progress}
	u.client = u.newClient()
	return u
}

// ProxyReachable 代理端口能否连通，用于决定是否回退为直连
func ProxyReachable(proxy string) bool {
	pu, err := url.Parse(proxy)
	if err != nil || pu.Host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", pu.Host, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// newClient 显式指定代理，不继承环境变量；重定向只允许保持 https
func (u *Updater) newClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if u.opt.Proxy != "" {
		if pu, err := url.Parse(u.opt.Proxy); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	secure := strings.HasPrefix(u.opt.BaseURL, "https://")
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if secure && req.URL.Scheme != "https" {
				return errors.New("拒绝从 https 重定向到非 https 地址")
			}
			if len(via) >= 10 {
				return errors.New("重定向次数过多")
			}
			return nil
		},
	}
}

func (u *Updater) versionsDir() string {
	return filepath.Join(u.opt.Home, ".local", "share", "claude", "versions")
}

func (u *Updater) cacheDir() string {
	return filepath.Join(u.opt.Home, ".local", "share", "claude", "update-cache")
}

// launcherName 官方安装器在用户目录下放置的启动文件名
func launcherName() string {
	if runtime.GOOS == "windows" {
		return "claude.exe"
	}
	return "claude"
}

// Current 当前启用的版本：~/.local/bin/claude（Windows 为 claude.exe）指向的版本文件名
func Current(home string) string {
	target, err := filepath.EvalSymlinks(filepath.Join(home, ".local", "bin", launcherName()))
	if err != nil {
		return ""
	}
	if v := filepath.Base(target); versionRe.MatchString(v) {
		return v
	}
	return ""
}

// Result 更新结果
type Result struct {
	Version  string
	Platform string
	Cached   bool // 本地已有校验通过的文件，跳过了下载
}

// ResolveProxy 决定使用的代理：配置了直连或代理不可用时返回空串（直连），并说明原因
func ResolveProxy(proxy string, direct bool) (string, string) {
	switch {
	case direct:
		return "", "已设置直连"
	case proxy == "":
		return "", "未配置代理"
	case !ProxyReachable(proxy):
		return "", "代理 " + proxy + " 不可用，改为直连"
	}
	return proxy, "经由代理 " + proxy
}

// Run 完整执行一次更新
func (u *Updater) Run(ctx context.Context, target string) (Result, error) {
	var res Result
	if !ValidTarget(target) {
		return res, fmt.Errorf("无效的目标版本：%s", target)
	}
	platform := u.opt.Platform
	if platform == "" {
		p, err := Platform()
		if err != nil {
			return res, err
		}
		platform = p
	}
	res.Platform = platform

	version := target
	if !versionRe.MatchString(target) {
		u.emit(Event{Stage: StageResolve, Msg: "查询 " + target + " 对应的版本号"})
		body, err := u.fetch(ctx, u.opt.BaseURL+"/"+target)
		if err != nil {
			return res, fmt.Errorf("获取版本号失败：%w", err)
		}
		version = strings.TrimSpace(string(body))
		if !versionRe.MatchString(version) {
			return res, errors.New("服务端返回了无效版本号")
		}
	}
	res.Version = version
	u.emit(Event{Stage: StageManifest, Msg: fmt.Sprintf("目标版本 %s，平台 %s", version, platform)})

	checksum, binary, err := u.release(ctx, version, platform)
	if err != nil {
		return res, err
	}
	if u.opt.DryRun {
		u.emit(Event{Stage: StageDone, Msg: "演练：发布清单校验值有效，将下载并安装 " + version})
		return res, nil
	}

	if err := os.MkdirAll(u.versionsDir(), 0o755); err != nil {
		return res, err
	}
	if err := os.MkdirAll(u.cacheDir(), 0o755); err != nil {
		return res, err
	}
	dest := filepath.Join(u.versionsDir(), version+binaryExt(binary))
	if err := regularOrMissing(dest); err != nil {
		return res, err
	}
	if matches(dest, checksum) {
		res.Cached = true
		u.emit(Event{Stage: StageDownload, Msg: "本地已有该版本且校验通过，跳过下载"})
	} else {
		part := filepath.Join(u.cacheDir(), version+"-"+platform+binaryExt(binary)+".part")
		url := fmt.Sprintf("%s/%s/%s/%s", u.opt.BaseURL, version, platform, binary)
		if err := u.download(ctx, url, part, checksum); err != nil {
			return res, err
		}
		// 半成品只留在缓存目录，校验通过后才移入版本目录
		if err := makeExecutable(part); err != nil {
			return res, err
		}
		if err := os.Rename(part, dest); err != nil {
			return res, err
		}
	}
	if err := makeExecutable(dest); err != nil {
		return res, err
	}

	if err := u.install(ctx, dest, version); err != nil {
		return res, err
	}
	u.emit(Event{Stage: StageDone, Msg: "版本 " + version + " 安装完成"})
	return res, nil
}

// fetch 获取小体积的元数据，带有限次重试
func (u *Updater) fetch(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			if err := sleep(ctx, 2*time.Second); err != nil {
				return nil, err
			}
		}
		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		body, err := u.get(reqCtx, url)
		cancel()
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (u *Updater) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// binaryNameRe 发布清单给出的文件名只允许 claude 或 claude.exe，避免路径穿越
var binaryNameRe = regexp.MustCompile(`^claude(\.exe)?$`)

// defaultBinary 清单未给出文件名时的回退：Windows 平台为 claude.exe，其余为 claude
func defaultBinary(platform string) string {
	if strings.HasPrefix(platform, "win32-") {
		return "claude.exe"
	}
	return "claude"
}

// binaryExt 版本目录与缓存文件保留 .exe 后缀，其余平台不带后缀
func binaryExt(binary string) string {
	return filepath.Ext(binary)
}

// release 从发布清单读取指定平台的 SHA-256 与二进制文件名。
// 较新的清单带 binary 字段；旧清单没有该字段时按平台回退。
func (u *Updater) release(ctx context.Context, version, platform string) (checksum, binary string, err error) {
	body, err := u.fetch(ctx, fmt.Sprintf("%s/%s/manifest.json", u.opt.BaseURL, version))
	if err != nil {
		return "", "", fmt.Errorf("获取发布清单失败：%w", err)
	}
	var m struct {
		Platforms map[string]struct {
			Binary   string `json:"binary"`
			Checksum string `json:"checksum"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", "", fmt.Errorf("发布清单无效：%w", err)
	}
	p, ok := m.Platforms[platform]
	if !ok {
		return "", "", fmt.Errorf("发布清单不支持平台 %s", platform)
	}
	if !checksumRe.MatchString(p.Checksum) {
		return "", "", errors.New("发布清单中的 SHA-256 无效")
	}
	binary = p.Binary
	if binary == "" {
		binary = defaultBinary(platform)
	}
	if !binaryNameRe.MatchString(binary) {
		return "", "", fmt.Errorf("发布清单中的二进制文件名无效：%s", binary)
	}
	return p.Checksum, binary, nil
}

// makeExecutable 给已下载的二进制加上可执行权限。
// Windows 不依赖 Unix 权限位，Chmod 在部分文件系统上还会失败，因此跳过。
func makeExecutable(p string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(p, 0o755)
}

// regularOrMissing 路径不存在，或是普通文件（不能是链接或目录）
func regularOrMissing(p string) error {
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("路径异常，不是普通文件：%s", p)
	}
	return nil
}

// matches 文件存在、非空、非链接且 SHA-256 一致
func matches(p, checksum string) bool {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == checksum
}

// install 调用新版本自带的 install 子命令完成安装与链接切换
func (u *Updater) install(ctx context.Context, dest, version string) error {
	u.emit(Event{Stage: StageInstall, Msg: fmt.Sprintf("调用官方安装，最长等待 %d 秒", int(u.opt.InstallTimeout.Seconds()))})
	ictx, cancel := context.WithTimeout(ctx, u.opt.InstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ictx, dest, "install", version)
	cmd.Env = u.installEnv()
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ictx.Err() == context.DeadlineExceeded {
		return errors.New("安装超时；已下载的文件会保留，检查网络后重试即可")
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return fmt.Errorf("安装失败，退出码：%d %s；已下载的文件会保留", code, msg)
	}
	return nil
}

// installEnv 安装子命令使用与下载一致的代理设置
func (u *Updater) installEnv() []string {
	proxyKeys := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		// Windows 上官方安装器按 USERPROFILE 落盘，和 HOME 一起改到指定家目录
		if proxyKeys[k] || k == "HOME" || (runtime.GOOS == "windows" && k == "USERPROFILE") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+u.opt.Home)
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+u.opt.Home)
	}
	if u.opt.Proxy == "" {
		return append(env, "NO_PROXY=*", "no_proxy=*")
	}
	p := u.opt.Proxy
	return append(env, "HTTP_PROXY="+p, "HTTPS_PROXY="+p, "http_proxy="+p, "https_proxy="+p)
}

// sleep 可被取消的等待
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
