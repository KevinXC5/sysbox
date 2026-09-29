package devcache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// locator 定位各工具的缓存目录。优先询问工具本身，其次读环境变量，最后用平台默认位置。
// 所有外部依赖都可注入，测试不读取本机真实目录。
type locator struct {
	home     string
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	runner   sysx.Runner
	ctx      context.Context // immutable context of a discovery pass

	mu    sync.Mutex
	cache map[string][]string // 命令输出缓存，删除前复核时不必重复启动外部命令
}

func newLocator(home string) *locator {
	return &locator{
		home: home, goos: goos, getenv: os.Getenv, lookPath: exec.LookPath, runner: sysx.ExecRunner{},
	}
}

// ask 执行查询命令，返回每行输出。命令不存在、超时或失败时返回空。
func (l *locator) ask(name string, args ...string) []string {
	key := name + " " + strings.Join(args, " ")
	l.mu.Lock()
	defer l.mu.Unlock()
	if out, ok := l.cache[key]; ok {
		return out
	}
	var lines []string
	if _, err := l.lookPath(name); err == nil {
		parent := l.ctx
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		out, err := l.runner.Run(ctx, sysx.C(name, args...))
		cancel()
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	if l.cache == nil {
		l.cache = map[string][]string{}
	}
	l.cache[key] = lines
	return lines
}

// has 命令是否可用
func (l *locator) has(name string) bool {
	_, err := l.lookPath(name)
	return err == nil
}

// pick 返回第一个可用的候选路径：必须是绝对路径，且目录真实存在（不跟随末级链接判断）。
// 命令输出可能是提示文字而不是路径，也会在这里被过滤掉。
func pick(candidates ...string) string {
	for _, c := range candidates {
		if c == "" || !filepath.IsAbs(c) {
			continue
		}
		c = filepath.Clean(c)
		if fi, err := os.Lstat(c); err == nil && (fi.IsDir() || fi.Mode()&os.ModeSymlink != 0) {
			return c
		}
	}
	return ""
}

// line 取命令输出的第 i 行，不存在时返回空
func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// local Windows 的 %LOCALAPPDATA%
func (l *locator) local() string {
	if v := l.getenv("LOCALAPPDATA"); v != "" {
		return v
	}
	return filepath.Join(l.home, "AppData", "Local")
}

// caches 平台的用户缓存根：macOS 为 ~/Library/Caches，Windows 为 %LOCALAPPDATA%
func (l *locator) caches() string {
	if l.goos == "windows" {
		return l.local()
	}
	return filepath.Join(l.home, "Library", "Caches")
}

// envOr 环境变量非空时用它，否则用默认值
func (l *locator) envOr(key, def string) string {
	if v := l.getenv(key); v != "" {
		return v
	}
	return def
}
