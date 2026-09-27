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
	for _, r := range versionRules {
		sc.scanVersions(r, progress)
	}
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
	if !plainDir(r.root) || filepath.Dir(item) != r.root {
		return errors.New("所在目录已改变")
	}
	if isSymlink(item) {
		return errors.New("已变成符号链接")
	}
	id, err := identify(item)
	if err != nil {
		return err
	}
	if id != r.id {
		return errors.New("文件已被替换")
	}
	if r.activeLink != "" {
		active, err := filepath.EvalSymlinks(r.activeLink)
		if err != nil || !isSymlink(r.activeLink) {
			return errors.New("当前版本链接异常")
		}
		if filepath.Dir(active) != r.root || active == item {
			return errors.New("当前版本已切换到该条目")
		}
		for _, o := range r.otherLinks {
			if t, err := filepath.EvalSymlinks(o); err != nil || t != active {
				return errors.New("命令链接不一致")
			}
		}
	}
	_, latest, err := usage(item, r.activeLink != "")
	if err != nil {
		return err
	}
	if now.Sub(latest) < r.minAge {
		return errors.New("扫描后又有修改")
	}
	return nil
}

func (s *Source) Notes() []string {
	return []string{
		fmt.Sprintf("只删除 %d 天未修改的缓存与日志；会话、凭据、插件和配置不在范围内", s.KeepDays),
		"旧版本会保留当前版本与最高版本",
		"删除前逐项复核，目标有变化会自动跳过",
	}
}

func (s *Source) Tip() string {
	return "大小按目录项占用估算，存在硬链接时实际释放可能更少"
}
