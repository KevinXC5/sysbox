package agentjunk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// DefaultKeepDays 缓存与日志默认的保留天数
const DefaultKeepDays = 30

// Source agent 垃圾清理数据源
type Source struct {
	Home     string
	KeepDays int
	Runner   sysx.Runner
	Now      func() time.Time
}

// NewSource 使用当前用户家目录；keepDays 不大于 0 时取默认值
func NewSource(keepDays int) (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	if keepDays <= 0 {
		keepDays = DefaultKeepDays
	}
	return &Source{Home: home, KeepDays: keepDays, Runner: sysx.ExecRunner{}, Now: time.Now}, nil
}

var _ cleanup.Source = (*Source)(nil)

func (s *Source) Title() string { return "Agent 垃圾" }

func (s *Source) Categories() []cleanup.Category {
	cats := append([]cleanup.Category(nil), categories...)
	cats[CatRecent].Sub = fmt.Sprintf("%d 天内有修改", s.KeepDays)
	return cats
}

func (s *Source) Scan(progress func(string)) ([]cleanup.Item, error) {
	sc := &scanner{home: s.Home, keep: time.Duration(s.KeepDays) * 24 * time.Hour, now: s.Now()}
	sc.scanDirs(cacheDirs, CatCache, progress)
	sc.scanDirs(logDirs, CatLog, progress)
	for _, r := range versionRules() {
		sc.scanVersions(r, progress)
	}
	sc.scanGrok(progress)
	sc.inspectBinaries(progress)
	sort.SliceStable(sc.items, func(i, j int) bool { return sc.items[i].Path < sc.items[j].Path })
	return sc.items, nil
}

func (s *Source) Roots() []cleanup.Root { return nil }

// Check 相关 agent 正在运行时只提醒，不阻止：清理的都是保留期外的内容
func (s *Source) Check() *cleanup.Notice {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	procs, err := sysx.FindProcs(ctx, s.Runner, sysx.ByName(agentProcs...))
	if err != nil || len(procs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	n := &cleanup.Notice{Title: "以下 agent 正在运行，建议先退出"}
	for _, p := range procs {
		if !seen[p.Name()] {
			seen[p.Name()] = true
			n.Lines = append(n.Lines, p.Name())
		}
	}
	return n
}

// Remove 删除前逐项复核：路径、父目录、inode、版本链接和保留期都必须与扫描时一致
func (s *Source) Remove(it cleanup.Item) error {
	if !it.Selectable {
		return errors.New("条目不可手动删除")
	}
	r, ok := it.Ref.(ref)
	if !ok {
		return errors.New("条目缺少复核信息，拒绝删除")
	}
	if err := r.stillSafe(it.Path, s.Now()); err != nil {
		return fmt.Errorf("目标已变化，跳过：%w", err)
	}
	if it.IsDir {
		return os.RemoveAll(it.Path)
	}
	return os.Remove(it.Path)
}

func (r ref) stillSafe(item string, now time.Time) error {
	// 目录名在 Windows 上大小写不敏感，短路径展开后也不能按字符串判成“已改变”
	if !plainDir(r.root) || !samePath(filepath.Dir(item), r.root) {
		return errors.New("所在目录已改变")
	}
	if isSymlink(item) != r.link {
		return errors.New("链接类型已改变")
	}
	id, err := identify(item)
	if err != nil {
		return err
	}
	if id != r.id {
		return errors.New("文件已被替换")
	}
	if r.copied {
		// 入口必须仍是独立的普通文件；变成链接后可能正指向该条目
		if fi, err := os.Lstat(r.activeLink); err != nil || !fi.Mode().IsRegular() {
			return errors.New("当前版本入口已改变")
		}
	} else if r.activeLink != "" {
		// 扫描时靠链接确认的当前版本，删除前必须仍是链接
		if !isSymlink(r.activeLink) {
			return errors.New("当前版本链接异常")
		}
		active, err := filepath.EvalSymlinks(r.activeLink)
		if err != nil {
			return errors.New("当前版本链接异常")
		}
		dirs := r.activeDirs
		if len(dirs) == 0 {
			dirs = []string{r.root}
		}
		if !inDirs(active, dirs) || samePath(active, item) {
			return errors.New("当前版本已切换到该条目")
		}
	}
	// 命令链接只删链接本身，已核对过 inode 与当前版本，不看修改时间
	if r.link {
		return nil
	}
	_, latest, err := usage(item, r.activeLink != "")
	if err != nil {
		return err
	}
	// 手动勾选的近期条目不要求已过保留期，但仍拒绝扫描之后又被改过的目标
	if r.manual {
		if latest.After(r.seen) {
			return errors.New("扫描后又有修改")
		}
		return nil
	}
	if now.Sub(latest) < r.minAge {
		return errors.New("扫描后又有修改")
	}
	return nil
}

func (s *Source) Notes() []string {
	return []string{
		fmt.Sprintf("默认只勾选 %d 天未修改的缓存；近期缓存、日志和旧版本可手动勾选", s.KeepDays),
		"当前版本、最高版本（Grok 只保留当前版本）、会话、凭据、插件和配置不在扫描范围内",
		"指向外部的符号链接和检查失败的条目不能手动纳入",
		"删除前逐项复核，目标有变化会自动跳过",
	}
}

func (s *Source) Tip() string {
	return "大小按目录项占用估算，存在硬链接时实际释放可能更少"
}
