// Package devcache 清理包管理器与构建工具的缓存：Go、npm、pnpm、Yarn、Bun、pip、uv、
// Gradle、Maven、Cargo、Homebrew。只影响下次安装速度的缓存默认勾选；
// 构建依赖、需要联网重新下载的目录默认不勾选。
package devcache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// goos 当前平台，测试可替换
var goos = runtime.GOOS

// 分类下标，与 categories 顺序一致
const (
	CatClean    = iota // 可清理：只影响下次安装或构建的速度
	CatDownload        // 重新下载：构建依赖它，删除后需要联网
	CatSkip            // 跳过：符号链接或无法读取
)

var categories = []cleanup.Category{
	CatClean:    {Label: "可清理", Sub: "只影响下次速度", Tone: cleanup.ToneGood, Primary: true},
	CatDownload: {Label: "重新下载", Sub: "构建时需要联网", Tone: cleanup.ToneWarn},
	CatSkip:     {Label: "跳过", Sub: "链接或无法读取", Tone: cleanup.ToneMuted},
}

// procs 各工具的进程名。清理时这些进程在运行，正在进行的安装或构建可能失败。
var procs = map[string][]string{
	"Go":       {"go"},
	"npm":      {"npm", "npx"},
	"pnpm":     {"pnpm"},
	"Yarn":     {"yarn"},
	"Bun":      {"bun"},
	"pip":      {"pip", "pip3"},
	"uv":       {"uv"},
	"Gradle":   {"gradle"},
	"Maven":    {"mvn"},
	"Cargo":    {"cargo", "rustc"},
	"Homebrew": {"brew"},
}

// ErrNone 本机没有找到任何支持的缓存
var ErrNone = errors.New("没有找到 Go、npm、Gradle 等开发工具的缓存目录")

// cmdTimeout 官方清理命令的最长执行时间
const cmdTimeout = 5 * time.Minute

// Source 开发缓存清理数据源
type Source struct {
	loc    *locator
	Runner sysx.Runner
}

// NewSource 使用当前用户家目录
func NewSource() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	return &Source{loc: newLocator(home), Runner: sysx.ExecRunner{}}, nil
}

// ref 扫描时记下的目录标识，删除前确认没有被替换
type ref struct{ id fsx.FileID }

var _ cleanup.Source = (*Source)(nil)
var _ cleanup.ItemChecker = (*Source)(nil)

func (s *Source) Title() string                  { return "开发缓存" }
func (s *Source) Categories() []cleanup.Category { return categories }

func (s *Source) Scan(progress func(string)) ([]cleanup.Item, error) {
	return s.ScanContext(context.Background(), progress)
}

func (s *Source) ScanContext(ctx context.Context, progress func(string)) ([]cleanup.Item, error) {
	progress("查找 Go、npm、Gradle 等工具的缓存")
	// Each pass owns its context and command cache. Do not retain a cancelled
	// scan context for deletion-time discovery.
	loc := &locator{home: s.loc.home, goos: s.loc.goos, getenv: s.loc.getenv,
		lookPath: s.loc.lookPath, runner: s.loc.runner, ctx: ctx}
	es := loc.entries()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.loc.mu.Lock()
	s.loc.cache = loc.cache
	s.loc.mu.Unlock()
	if len(es) == 0 {
		return nil, ErrNone
	}
	items := make([]cleanup.Item, 0, len(es))
	for _, e := range es {
		it := toItem(e)
		if err := s.loc.safePath(e.path); err != nil {
			it = locked(it, err.Error())
		}
		items = append(items, it)
	}
	return items, nil
}

// toItem 把缓存目录转成条目。自身是符号链接或读不到标识的不可勾选。
func toItem(e entry) cleanup.Item {
	it := cleanup.Item{
		Path: e.path, Name: e.name, Group: e.group, Category: e.cat, Note: e.note, IsDir: true,
		Selectable: true, Selected: e.cat == CatClean, Irreversible: e.irreversible,
	}
	fi, err := os.Lstat(e.path)
	switch {
	case err != nil:
		return locked(it, "无法读取")
	case fi.Mode()&fs.ModeSymlink != 0:
		return locked(it, "符号链接，不跟随目标")
	}
	id, err := fsx.Identify(e.path)
	if err != nil {
		return locked(it, "无法读取文件标识")
	}
	it.Ref = ref{id}
	return it
}

func locked(it cleanup.Item, reason string) cleanup.Item {
	it.Category, it.Selectable, it.Selected, it.Irreversible = CatSkip, false, false, false
	it.Note = it.Note + "（" + reason + "，不可选）"
	it.Sized = true
	return it
}

func (s *Source) Roots() []cleanup.Root { return nil }

// Check 检查全部工具
func (s *Source) Check() *cleanup.Notice {
	var groups []string
	for g := range procs {
		groups = append(groups, g)
	}
	return s.check(groups)
}

// CheckItems 只提示与所选缓存相关、且正在运行的工具。只提醒不阻止：缓存都能重新生成。
func (s *Source) CheckItems(items []cleanup.Item) *cleanup.Notice {
	var groups []string
	for _, it := range items {
		if !slices.Contains(groups, it.Group) {
			groups = append(groups, it.Group)
		}
	}
	return s.check(groups)
}

func (s *Source) check(groups []string) *cleanup.Notice {
	var names []string
	for _, g := range groups {
		names = append(names, procs[g]...)
	}
	if len(names) == 0 || s.Runner == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	found, err := sysx.FindProcs(ctx, s.Runner, sysx.ByName(names...))
	if err != nil || len(found) == 0 {
		return nil
	}
	n := &cleanup.Notice{Title: "以下工具正在运行，进行中的安装或构建可能失败"}
	seen := map[string]bool{}
	for _, p := range found {
		if !seen[p.Name()] {
			seen[p.Name()] = true
			n.Lines = append(n.Lines, p.Name())
		}
	}
	return n
}

// Remove 删除前复核：路径必须仍在重新扫描的结果里、分类不变、不是链接、标识与扫描时一致
func (s *Source) Remove(it cleanup.Item) error {
	r, ok := it.Ref.(ref)
	if !ok || !it.Selectable {
		return errors.New("条目缺少复核信息，拒绝删除")
	}
	var e *entry
	for _, x := range s.loc.entries() {
		if pathKey(x.path) == pathKey(it.Path) && x.cat == it.Category {
			e = &x
			break
		}
	}
	if e == nil {
		return fmt.Errorf("条目不在本次扫描结果中：%s", it.Path)
	}
	if err := s.loc.safePath(e.path); err != nil {
		return fmt.Errorf("拒绝删除：%w", err)
	}
	fi, err := os.Lstat(e.path)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return errors.New("目标已变为链接或文件，跳过")
	}
	if id, err := fsx.Identify(e.path); err != nil || id != r.id {
		return errors.New("目录已被替换，跳过")
	}
	if e.cmd != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		if _, err := s.loc.runner.Run(ctx, *e.cmd); err != nil {
			return fmt.Errorf("%s 执行失败：%w", e.cmd, err)
		}
		return nil
	}
	return fsx.ForceRemoveAll(e.path)
}

func (s *Source) Notes() []string {
	return []string{
		"默认只勾选删除后仅影响下次安装速度的缓存",
		"Go 模块、pnpm store、Gradle 依赖、Maven 仓库等构建依赖默认不勾选，删除后需要联网重新下载",
		"Go、uv 调用官方清理命令，其余直接删除目录；符号链接不跟随",
	}
}

func (s *Source) Tip() string {
	return "缓存会在下次安装或构建时按需重新生成"
}
