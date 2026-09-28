// Package jetbrains 清理 JetBrains 本地缓存。
// 只处理缓存目录和日志目录，不碰配置、插件本体等用户数据。
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

// Options 扫描与删除的根路径。
// LogInsideCache 为真时日志在各产品缓存目录的 log 子目录里（Windows），
// 为假时日志是独立根目录（macOS 的 ~/Library/Logs/JetBrains）。
type Options struct {
	Home           string
	CacheRoot      string
	LogRoot        string
	LogInsideCache bool
	AppSupport     string // 配置与插件本体，只展示不删除
	// 完成页上的根目录标签，空时使用 macOS 默认文案
	CacheLabel  string
	LogLabel    string
	ConfigLabel string
}

// ErrNoCache 缓存根目录不存在
var ErrNoCache = errors.New("找不到 JetBrains 缓存目录")

// DefaultOptions 返回当前平台的默认路径，根路径先解析符号链接。
// lookup 注入环境变量，测试不依赖真实系统。
func DefaultOptions() (Options, error) {
	return optionsFrom(os.UserHomeDir, os.Getenv)
}

// optionsFrom 按平台拼出缓存、日志和配置根。home 失败时直接返回错误。
func optionsFrom(homeDir func() (string, error), lookup func(string) string) (Options, error) {
	home, err := homeDir()
	if err != nil {
		return Options{}, err
	}
	return platformOptions(resolve(home), lookup), nil
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

		// acp-agents 展开后按 agent 只保留最新版本
		if top.Name() == acpAgentsName && top.IsDir() && top.Type()&fs.ModeSymlink == 0 {
			items = append(items, classifyAgents(p, "JetBrains")...)
			continue
		}
		// 顶层整目录跳过
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
			cp := filepath.Join(p, c.Name())
			// 日志在缓存目录内部时单独归到 Logs，避免和缓存子目录重复统计或按未知条目漏掉
			if o.LogInsideCache && c.Name() == logDirName && c.IsDir() && c.Type()&fs.ModeSymlink == 0 {
				items = append(items, with(cleanup.Item{
					Path: cp, Name: c.Name(), Group: "Logs/" + top.Name(), IsDir: true,
				}, CatClean, noteLog))
				continue
			}
			if c.Name() == acpAgentsName && c.IsDir() && c.Type()&fs.ModeSymlink == 0 {
				items = append(items, classifyAgents(cp, top.Name())...)
				continue
			}
			items = append(items, classifyChild(cp, top.Name(), c))
		}
	}

	// macOS 日志是独立根目录，整棵可删，IDE 下次启动会重建。
	// 日志在缓存内部时上面已经处理，这里不再扫第二遍。
	if !o.LogInsideCache && o.LogRoot != "" {
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
