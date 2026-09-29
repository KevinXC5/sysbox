// Package vscode 清理 VS Code 与 Insiders 的可重建缓存、确定落后的扩展二进制，
// 以及能用产品版本确认的旧 CLI / server。不碰用户设置、会话和未知目录。
package vscode

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
	CatClean = iota // 可清理：可重建缓存，或已确认的旧二进制
	CatKeep         // 保留：用户数据、当前版本、证据不足
	CatSkip         // 跳过：索引异常时整组不清理
)

var categories = []cleanup.Category{
	CatClean: {Label: "可清理", Sub: "缓存与旧版本", Tone: cleanup.ToneGood, Primary: true},
	CatKeep:  {Label: "保留", Sub: "设置与当前版本", Tone: cleanup.ToneInfo},
	CatSkip:  {Label: "跳过", Sub: "无法确认", Tone: cleanup.ToneWarn},
}

// Channel 一个产品通道：稳定版或 Insiders。两者规则相同，目录名不同。
type Channel struct {
	Name      string
	AppRoot   string
	ExtRoot   string
	CLIRoots  []string
	ProcNames []string
	AppLabel  string
	ExtLabel  string
}

// Options 扫描与删除使用的路径。测试直接构造，不依赖本机目录。
type Options struct {
	Home     string
	Channels []Channel
}

// ErrNoData 两个通道的应用数据都不存在
var ErrNoData = errors.New("找不到 VS Code 数据目录")

// DefaultOptions 返回当前平台的默认路径
func DefaultOptions() (Options, error) {
	return optionsFrom(os.UserHomeDir, os.Getenv)
}

func optionsFrom(homeDir func() (string, error), lookup func(string) string) (Options, error) {
	home, err := homeDir()
	if err != nil {
		return Options{}, err
	}
	return platformOptions(resolve(home), lookup), nil
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Classify 扫描全部通道。大小留给界面统计。
func Classify(o Options) ([]cleanup.Item, error) {
	var items []cleanup.Item
	var any bool
	for _, ch := range o.Channels {
		if st, err := os.Stat(ch.AppRoot); err == nil && st.IsDir() {
			any = true
			items = append(items, classifyApp(ch)...)
		}
		if st, err := os.Stat(ch.ExtRoot); err == nil && st.IsDir() {
			any = true
			items = append(items, classifyExtensions(ch)...)
		}
		for _, root := range ch.CLIRoots {
			if st, err := os.Stat(root); err == nil && st.IsDir() {
				any = true
				items = append(items, classifyCLI(ch, root)...)
			}
		}
	}
	if !any {
		return nil, ErrNoData
	}
	return items, nil
}

func classifyApp(ch Channel) []cleanup.Item {
	entries, err := os.ReadDir(ch.AppRoot)
	if err != nil {
		return []cleanup.Item{kept(ch.AppRoot, ch.Name, ch.Name, true, noteUnread)}
	}
	var items []cleanup.Item
	for _, e := range entries {
		p := filepath.Join(ch.AppRoot, e.Name())
		name := e.Name()
		link := e.Type()&fs.ModeSymlink != 0
		switch {
		case link:
			items = append(items, kept(p, name, ch.Name, false, noteSymlink))
		case protectedDirs[name] != "":
			items = append(items, kept(p, name, ch.Name, e.IsDir(), protectedDirs[name]))
		case e.IsDir() && cleanCache[name] != "":
			items = append(items, clean(p, name, ch.Name, cleanCache[name]))
		default:
			// 文件和未知目录都不删：Cookies、Preferences、Local State 以及以后新增的目录
			items = append(items, kept(p, name, ch.Name, e.IsDir(), noteUnknown))
		}
	}
	return items
}

func classifyExtensions(ch Channel) []cleanup.Item {
	idx := loadIndex(ch.AppRoot, ch.ExtRoot)
	if !idx.ok {
		return []cleanup.Item{skipped(ch.ExtRoot, "extensions", ch.Name, true, idx.reason)}
	}
	obsolete, ok := readObsolete(ch.ExtRoot)
	if !ok {
		return []cleanup.Item{skipped(ch.ExtRoot, "extensions", ch.Name, true, ".obsolete 已损坏，无法确认废弃扩展")}
	}
	entries, err := os.ReadDir(ch.ExtRoot)
	if err != nil {
		return []cleanup.Item{skipped(ch.ExtRoot, "extensions", ch.Name, true, noteUnread)}
	}

	type inst struct {
		ext extInstall
		it  cleanup.Item
	}
	var all []inst
	var items []cleanup.Item
	for _, e := range entries {
		p := filepath.Join(ch.ExtRoot, e.Name())
		name := e.Name()
		if name == ".obsolete" || strings.HasPrefix(name, ".") {
			items = append(items, kept(p, name, ch.Name, e.IsDir(), "扩展目录元数据，保留"))
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			items = append(items, kept(p, name, ch.Name, false, noteUnknown))
			continue
		}
		id, ver, ok := readPackage(p)
		if !ok {
			items = append(items, kept(p, name, ch.Name, true, "缺少可用的 package.json，无法确认版本"))
			continue
		}
		all = append(all, inst{
			ext: extInstall{dir: name, id: id, version: ver, obsolete: obsolete[strings.ToLower(name)]},
			it:  cleanup.Item{Path: p, Name: name, Group: ch.Name + "/extensions", IsDir: true},
		})
	}

	// 同扩展同平台里的最高版本，以及索引正在引用的目录，都不能删
	latest := map[string]string{}
	for _, x := range all {
		k := x.ext.id.key()
		if prev, ok := latest[k]; !ok || olderThan(prev, x.ext.version) {
			latest[k] = x.ext.version
		}
	}
	for _, x := range all {
		refVer, referenced := idx.referenced[strings.ToLower(x.ext.dir)]
		switch {
		case referenced:
			// 默认索引和任一配置档引用都保留，未出现在索引里也不等于可删
			items = append(items, with(x.it, CatKeep, "配置档索引正在引用 "+refVer+"，保留"))
		case !olderThan(x.ext.version, latest[x.ext.id.key()]):
			items = append(items, with(x.it, CatKeep, "同平台最新版本，保留"))
		case x.ext.obsolete:
			items = append(items, with(x.it, CatClean, "已在 .obsolete 中标记，且同平台已有更高版本 "+latest[x.ext.id.key()]))
		default:
			items = append(items, with(x.it, CatClean, "同平台旧版本，已有更高版本 "+latest[x.ext.id.key()]))
		}
	}
	return items
}

// classifyCLI 只比较同一通道、同一质量（stable/insider）下能读到产品版本的安装。
// 目录名里的提交哈希不能当版本排序；读不到 product.json 或 package.json 就保留。
func classifyCLI(ch Channel, root string) []cleanup.Item {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []cleanup.Item{kept(root, filepath.Base(root), ch.Name+"/cli", true, noteUnread)}
	}
	type inst struct {
		name    string
		path    string
		version string
		quality string
	}
	var found []inst
	var items []cleanup.Item
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			items = append(items, kept(p, e.Name(), ch.Name+"/cli", e.IsDir(), noteUnknown))
			continue
		}
		ver, quality, ok := readInstallVersion(p)
		if !ok {
			items = append(items, kept(p, e.Name(), ch.Name+"/cli", true, "读不到产品版本，不能按哈希或修改时间删除"))
			continue
		}
		found = append(found, inst{name: e.Name(), path: p, version: ver, quality: quality})
		// 安装目录内部的可重建缓存单独列出，不把整个旧服务端当成默认可删
		items = append(items, classifyServerCache(p, ch.Name+"/cli/"+e.Name())...)
	}
	latest := map[string]string{}
	for _, x := range found {
		if prev, ok := latest[x.quality]; !ok || olderThan(prev, x.version) {
			latest[x.quality] = x.version
		}
	}
	for _, x := range found {
		it := cleanup.Item{Path: x.path, Name: x.name, Group: ch.Name + "/cli", IsDir: true}
		if !olderThan(x.version, latest[x.quality]) {
			items = append(items, with(it, CatKeep, x.quality+" 最新版本 "+x.version+"，保留"))
			continue
		}
		// 没有“当前正在使用”的引用，只能确认它比同通道最高版本旧。
		// 默认不勾选，避免删掉仍会被远程重连用到的服务端。
		it = with(it, CatKeep, x.quality+" 旧版本 "+x.version+"，已有 "+latest[x.quality]+"；缺少当前引用，重新连接需要重新下载")
		it.Category = CatSkip
		it.Selectable = true
		it.Selected = false
		it.Irreversible = true
		items = append(items, it)
	}
	return items
}

// classifyServerCache 只扫服务端安装根下的固定缓存名，不递归未知目录。
// 这些缓存可以本地重建，与“哪个提交正在被连接使用”无关。
func classifyServerCache(install, group string) []cleanup.Item {
	entries, err := os.ReadDir(install)
	if err != nil {
		return nil
	}
	var items []cleanup.Item
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		name := e.Name()
		note := cleanCache[name]
		if note == "" {
			continue
		}
		items = append(items, clean(filepath.Join(install, name), name, group, "服务端"+note))
	}
	return items
}

// readInstallVersion 从 product.json 或 package.json 读取版本与质量。哈希目录名被忽略。
func readInstallVersion(dir string) (version, quality string, ok bool) {
	for _, name := range []string{"product.json", "package.json", "server/product.json", "server/package.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var doc struct {
			Version string `json:"version"`
			Quality string `json:"quality"`
		}
		if err := jsonUnmarshal(b, &doc); err != nil || doc.Version == "" {
			continue
		}
		if _, parsed := parseVersion(doc.Version); !parsed {
			continue
		}
		q := strings.ToLower(doc.Quality)
		if q == "" {
			q = "stable"
		}
		return doc.Version, q, true
	}
	return "", "", false
}

func clean(path, name, group, note string) cleanup.Item {
	return with(cleanup.Item{Path: path, Name: name, Group: group, IsDir: true}, CatClean, note)
}

func kept(path, name, group string, dir bool, note string) cleanup.Item {
	return with(cleanup.Item{Path: path, Name: name, Group: group, IsDir: dir}, CatKeep, note)
}

func skipped(path, name, group string, dir bool, note string) cleanup.Item {
	return with(cleanup.Item{Path: path, Name: name, Group: group, IsDir: dir}, CatSkip, note)
}

// with 设置分类。只有可清理默认勾选。
// 保护项和索引异常整组跳过都不可手动纳入，因此不标不可恢复；
// 能手动删除的旧 CLI / 服务端在调用处单独设置。
func with(it cleanup.Item, cat int, note string) cleanup.Item {
	it.Category, it.Note = cat, note
	it.Selectable = cat == CatClean
	it.Selected = cat == CatClean
	it.Irreversible = false
	return it
}
