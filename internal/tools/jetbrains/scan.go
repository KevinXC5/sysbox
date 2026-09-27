// Package jetbrains 清理 JetBrains 本地缓存。
// 只处理 Caches/JetBrains 和 Logs/JetBrains，不碰 Application Support。
package jetbrains

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// 分类下标，与 categories 顺序一致
const (
	CatClean    = iota // 可清理：本地可重建的缓存和日志
	CatSkip            // 跳过：删除后需要重新下载
	CatKeep            // 保留：元数据、Local History
	CatLeftover        // 未覆盖：未识别的条目，只展示
)

var categories = []cleanup.Category{
	CatClean:    {Label: "可清理", Tone: cleanup.ToneGood, Primary: true},
	CatSkip:     {Label: "跳过", Sub: "需重新下载", Tone: cleanup.ToneWarn},
	CatKeep:     {Label: "保留", Sub: "元数据与历史", Tone: cleanup.ToneInfo},
	CatLeftover: {Label: "未覆盖", Sub: "只展示不删", Tone: cleanup.ToneMuted},
}

// Options 扫描与删除的根路径
type Options struct {
	Home       string
	CacheRoot  string
	LogRoot    string
	AppSupport string
}

// ErrNoCache 缓存根目录不存在
var ErrNoCache = errors.New("找不到 JetBrains 缓存目录")

// DefaultOptions 返回 macOS 下的默认路径，根路径先解析符号链接
func DefaultOptions() (Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Options{}, err
	}
	lib := filepath.Join(home, "Library")
	return Options{
		Home:       resolve(home),
		CacheRoot:  resolve(filepath.Join(lib, "Caches", "JetBrains")),
		LogRoot:    resolve(filepath.Join(lib, "Logs", "JetBrains")),
		AppSupport: resolve(filepath.Join(lib, "Application Support", "JetBrains")),
	}, nil
}

// resolve 解析符号链接；路径不存在时返回清理后的原路径
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Classify 遍历缓存与日志根目录并分类，不统计大小
func Classify(o Options) ([]cleanup.Item, error) {
	tops, err := os.ReadDir(o.CacheRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoCache
	}
	if err != nil {
		return nil, err
	}

	var items []cleanup.Item
	for _, top := range tops {
		p := filepath.Join(o.CacheRoot, top.Name())
		base := cleanup.Item{Path: p, Name: top.Name(), Group: "JetBrains", IsDir: top.IsDir()}

		// 顶层整目录跳过（如 acp-agents）
		if note, ok := skipRules[top.Name()]; ok {
			items = append(items, with(base, CatSkip, note))
			continue
		}
		// 顶层符号链接和普通文件都不深入
		if top.Type()&fs.ModeSymlink != 0 {
			items = append(items, with(base, CatLeftover, noteSymlink))
			continue
		}
		if !top.IsDir() {
			items = append(items, with(base, CatLeftover, noteUnknown))
			continue
		}

		children, err := os.ReadDir(p)
		if err != nil {
			items = append(items, with(base, CatLeftover, noteUnread))
			continue
		}
		for _, c := range children {
			items = append(items, classifyChild(filepath.Join(p, c.Name()), top.Name(), c))
		}
	}

	// 日志整棵可删，IDE 下次启动会重建
	if logs, err := os.ReadDir(o.LogRoot); err == nil {
		for _, l := range logs {
			items = append(items, with(cleanup.Item{
				Path:  filepath.Join(o.LogRoot, l.Name()),
				Name:  l.Name(),
				Group: "Logs",
				IsDir: l.IsDir(),
			}, CatClean, noteLog))
		}
	}
	return items, nil
}

// with 设置分类与说明；可清理分类默认勾选
func with(it cleanup.Item, cat int, note string) cleanup.Item {
	it.Category, it.Note = cat, note
	if cat == CatClean {
		it.Selectable, it.Selected = true, true
	}
	return it
}

// classifyChild 对 IDE 目录下的单个子条目分类
func classifyChild(p, group string, e fs.DirEntry) cleanup.Item {
	name := e.Name()
	it := cleanup.Item{Path: p, Name: name, Group: group, IsDir: e.IsDir()}
	isLink := e.Type()&fs.ModeSymlink != 0

	switch {
	case skipRules[name] != "":
		return with(it, CatSkip, skipRules[name])
	case keepRules[name] != "":
		return with(it, CatKeep, keepRules[name])
	case name == localHistoryName:
		// 默认保留，但允许手动纳入清理
		it = with(it, CatKeep, localHistoryNote)
		it.Selectable, it.Irreversible = true, true
		return it
	case isLink:
		return with(it, CatLeftover, noteSymlink)
	case e.IsDir() && cleanRules[name] != "":
		return with(it, CatClean, cleanRules[name])
	case e.Type().IsRegular() && strings.HasPrefix(name, "icon-cache") && strings.HasSuffix(name, ".db"):
		return with(it, CatClean, noteIconCache)
	case strings.HasSuffix(name, "-outbox"):
		return with(it, CatLeftover, noteOutbox)
	default:
		return with(it, CatLeftover, noteUnknown)
	}
}
