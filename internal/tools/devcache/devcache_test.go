package devcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// fakeRunner 按命令文本返回预设输出，并记录执行过的命令
type fakeRunner struct {
	out map[string]string
	ran *[]string
}

func (f fakeRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	if f.ran != nil {
		*f.ran = append(*f.ran, c.String())
	}
	if out, ok := f.out[c.String()]; ok {
		return out, nil
	}
	return "", errors.New("未预设的命令")
}

// fixture 在临时家目录下按 macOS 布局建出各工具缓存。tools 是“已安装”的命令。
func fixture(t *testing.T, tools ...string) (*Source, string, *[]string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		"Library/Caches/go-build",
		"go/pkg/mod/example.com/m@v1.0.0",
		".npm/_cacache", ".npm/_npx",
		"Library/Caches/pip",
		".gradle/caches/build-cache-1", ".gradle/caches/modules-2",
		".gradle/caches/8.5", ".gradle/caches/9.3.0", ".gradle/caches/jars-9",
		".gradle/daemon/8.5", ".gradle/daemon/9.3.0",
		".gradle/wrapper/dists/gradle-8.13-bin", ".gradle/wrapper/dists/gradle-9.3.0-bin", ".gradle/wrapper/dists/gradle-9.3.0-all",
		".m2/repository",
		"Library/Caches/Homebrew",
	} {
		mustMkdir(t, filepath.Join(home, d))
	}
	ran := &[]string{}
	l := &locator{
		home: home, goos: "darwin",
		getenv: func(string) string { return "" },
		lookPath: func(name string) (string, error) {
			for _, n := range tools {
				if n == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		// pnpm 的 shim 会把提示文字打到标准输出，不能当成路径
		runner: fakeRunner{ran: ran, out: map[string]string{
			"pnpm store path": "[pnpm] pnpm not found. Please install it first.\n",
		}},
	}
	return &Source{loc: l}, home, ran
}

func scan(t *testing.T, s *Source) map[string]cleanup.Item {
	t.Helper()
	items, err := s.Scan(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]cleanup.Item{}
	for _, it := range items {
		out[it.Group+"/"+it.Name] = it
	}
	return out
}

func TestDefaultSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例按 macOS 目录布局构造")
	}
	s, _, _ := fixture(t, "pnpm")
	got := scan(t, s)
	for _, k := range []string{"Go/构建缓存", "npm/下载缓存", "npm/npx 缓存", "pip/下载缓存", "Gradle/构建缓存", "Gradle/caches/8.5", "Gradle/daemon/8.5", "Homebrew/下载缓存"} {
		if it, ok := got[k]; !ok || it.Category != CatClean || !it.Selected {
			t.Errorf("%s 应默认勾选：%+v", k, it)
		}
	}
	for _, k := range []string{"Go/模块缓存", "Gradle/依赖缓存", "Gradle/gradle-8.13-bin", "Maven/本地仓库"} {
		if it, ok := got[k]; !ok || it.Category != CatDownload || it.Selected || !it.Selectable {
			t.Errorf("%s 应可选但默认不勾选：%+v", k, it)
		}
	}
	if !got["Maven/本地仓库"].Irreversible {
		t.Error("Maven 仓库可能含本地安装的构件，应按不可恢复警示")
	}
	// 最高版本的缓存、守护进程与发行包都保留；无法解析版本的目录不参与
	for _, k := range []string{"Gradle/caches/9.3.0", "Gradle/daemon/9.3.0", "Gradle/gradle-9.3.0-bin", "Gradle/gradle-9.3.0-all", "Gradle/caches/jars-9"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s 不应列出", k)
		}
	}
	if _, ok := got["pnpm/store"]; ok {
		t.Error("命令输出不是路径，且默认目录不存在时不应列出 pnpm")
	}
}

func TestAskedPathWins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例按 macOS 目录布局构造")
	}
	s, home, _ := fixture(t, "go")
	custom := filepath.Join(home, "custom-gocache")
	mustMkdir(t, custom)
	s.loc.runner = fakeRunner{out: map[string]string{
		"go env GOCACHE GOMODCACHE": custom + "\n" + filepath.Join(home, "missing") + "\n",
	}}
	got := scan(t, s)
	if got["Go/构建缓存"].Path != custom {
		t.Fatalf("应使用 go env 给出的构建缓存：%s", got["Go/构建缓存"].Path)
	}
	// go env 给出的模块缓存不存在时回退到 GOPATH 默认位置
	if want := filepath.Join(home, "go", "pkg", "mod"); got["Go/模块缓存"].Path != want {
		t.Fatalf("模块缓存应回退到 %s，实际 %s", want, got["Go/模块缓存"].Path)
	}
}

func TestRemoveUsesOfficialCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例按 macOS 目录布局构造")
	}
	s, home, ran := fixture(t, "go")
	s.loc.runner = fakeRunner{ran: ran, out: map[string]string{
		"go env GOCACHE GOMODCACHE": "\n\n",
		"go clean -cache":           "",
	}}
	it := scan(t, s)["Go/构建缓存"]
	if err := s.Remove(it); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*ran, ";"), "go clean -cache") {
		t.Fatalf("go 可用时应调用 go clean -cache，实际 %v", *ran)
	}
	if _, err := os.Stat(filepath.Join(home, "Library/Caches/go-build")); err != nil {
		t.Fatal("交给官方命令清理时不应再直接删目录")
	}
}

func TestRemoveReadOnlyTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例按 macOS 目录布局构造")
	}
	s, home, _ := fixture(t)
	mod := filepath.Join(home, "go", "pkg", "mod")
	f := filepath.Join(mod, "example.com", "m@v1.0.0", "go.mod")
	mustWrite(t, f)
	// Go 模块缓存里的文件和目录都是只读的
	if err := os.Chmod(f, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(f), 0o555); err != nil {
		t.Fatal(err)
	}
	it := scan(t, s)["Go/模块缓存"]
	if err := s.Remove(it); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mod); !os.IsNotExist(err) {
		t.Fatal("只读目录树应被删除")
	}
}

func TestRemoveRejectsForgedAndReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例按 macOS 目录布局构造")
	}
	s, home, _ := fixture(t)
	got := scan(t, s)
	forged := got["npm/下载缓存"]
	forged.Path = filepath.Join(home, ".ssh")
	mustMkdir(t, forged.Path)
	if err := s.Remove(forged); err == nil {
		t.Fatal("不在扫描结果里的路径必须拒绝")
	}
	pip := got["pip/下载缓存"]
	if err := os.RemoveAll(pip.Path); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, pip.Path)
	if err := s.Remove(pip); err == nil || !strings.Contains(err.Error(), "替换") {
		t.Fatalf("扫描后被替换的目录必须跳过：%v", err)
	}
	// 分类被篡改成默认可清理也不行
	m2 := got["Maven/本地仓库"]
	m2.Category = CatClean
	if err := s.Remove(m2); err == nil {
		t.Fatal("分类与重新扫描不一致时必须拒绝")
	}
}

func TestSymlinkCacheLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("创建符号链接需要额外权限")
	}
	s, home, _ := fixture(t)
	target := filepath.Join(home, "elsewhere")
	mustMkdir(t, target)
	mustMkdir(t, filepath.Join(home, ".bun", "install"))
	if err := os.Symlink(target, filepath.Join(home, ".bun", "install", "cache")); err != nil {
		t.Fatal(err)
	}
	it := scan(t, s)["Bun/安装缓存"]
	if it.Selectable || it.Category != CatSkip {
		t.Fatalf("符号链接缓存不能勾选：%+v", it)
	}
}

func TestVersionOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"8.5", "9.3.0", true},
		{"8.13", "8.5", false},
		{"8.14-rc-1", "8.14", true},
		{"9.3.0", "9.3", false},
		{"jars-9", "9.3", false},
	} {
		if got := olderThan(c.a, c.b); got != c.want {
			t.Errorf("olderThan(%s, %s) = %v", c.a, c.b, got)
		}
	}
	if wrapperVersion("gradle-8.13-bin") != "8.13" || wrapperVersion("gradle-8.14-rc-1-all") != "8.14-rc-1" || wrapperVersion("other") != "" {
		t.Error("wrapper 目录名解析错误")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
