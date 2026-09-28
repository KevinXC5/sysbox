package agentjunk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// ref 候选条目在删除前复核所需的信息
type ref struct {
	root       string        // 条目所在目录，必须保持不变
	id         identity      // 扫描时的设备号与 inode
	minAge     time.Duration // 删除时仍须满足的最短未修改时长
	activeLink string        // 版本类条目：当前版本链接
	otherLinks []string      // 版本类条目：其他必须一致的命令链接
}

// scanner 一次扫描的上下文
type scanner struct {
	home  string
	keep  time.Duration // 缓存与日志的保留期
	now   time.Time
	items []cleanup.Item
}

// errItem 记录一个检查失败的路径
func (s *scanner) errItem(p, agent string, err error) {
	s.items = append(s.items, cleanup.Item{
		Path: p, Name: filepath.Base(p), Group: agent, Category: CatError, Note: err.Error(),
	})
}

// scanDirs 扫描缓存或日志目录下的一级条目
func (s *scanner) scanDirs(rules []dirRule, cat int, progress func(string)) {
	for _, r := range rules {
		root := filepath.Join(s.home, r.Rel)
		if !plainDir(root) {
			continue
		}
		progress("扫描 ~/" + r.Rel)
		entries, err := os.ReadDir(root)
		if err != nil {
			s.errItem(root, r.Agent, err)
			continue
		}
		for _, e := range entries {
			s.add(filepath.Join(root, e.Name()), root, r.Agent, cat, s.keep, "", nil)
		}
	}
}

// add 统计条目并归类：超过保留期的成为候选，否则归入近期使用
func (s *scanner) add(item, root, agent string, cat int, minAge time.Duration, activeLink string, otherLinks []string) {
	if isSymlink(item) {
		return
	}
	size, latest, err := usage(item, activeLink != "")
	it := cleanup.Item{Path: item, Name: filepath.Base(item), Group: agent, Size: size, Sized: true}
	if fi, statErr := os.Lstat(item); statErr == nil {
		it.IsDir = fi.IsDir()
	}
	switch {
	case errors.Is(err, errForeignLink):
		it.Category, it.Note = CatReview, "目录内含指向外部的符号链接，不自动清理"
	case err != nil:
		s.errItem(item, agent, err)
		return
	case s.now.Sub(latest) < minAge:
		days := int(s.now.Sub(latest).Hours() / 24)
		it.Category, it.Note = CatRecent, fmt.Sprintf("%d 天前有修改，未到 %d 天保留期", days, int(minAge.Hours()/24))
	default:
		id, err := identify(item)
		if err != nil {
			s.errItem(item, agent, err)
			return
		}
		it.Category, it.Selectable = cat, true
		it.Selected = cat != CatLog // 日志默认不勾选
		it.Note = noteFor(cat, latest, s.now)
		it.Ref = ref{root: root, id: id, minAge: minAge, activeLink: activeLink, otherLinks: otherLinks}
	}
	s.items = append(s.items, it)
}

func noteFor(cat int, latest, now time.Time) string {
	days := int(now.Sub(latest).Hours() / 24)
	switch cat {
	case CatLog:
		return fmt.Sprintf("日志，%d 天未修改", days)
	case CatOld:
		return "非当前版本，也不是最新版本"
	}
	return fmt.Sprintf("缓存，%d 天未修改，可重新生成", days)
}

// scanVersions 找出可确认的旧版本：同时保留当前版本和最高版本，防止更新期间误删刚下载的版本
func (s *scanner) scanVersions(r versionRule, progress func(string)) {
	root := filepath.Join(s.home, r.Dir)
	link := filepath.Join(s.home, r.Link)
	if !plainDir(root) {
		return
	}
	// 入口不存在时没有可确认的当前版本，整组跳过，避免把全部历史版本都标成待核查
	if _, err := os.Lstat(link); err != nil {
		return
	}
	progress("检查 " + r.Agent + " 旧版本")
	review := func(note string) {
		s.items = append(s.items, cleanup.Item{Path: link, Name: r.Link, Group: r.Agent, Category: CatReview, Note: note})
	}
	// Windows 原生安装把入口复制成普通 exe，无法用链接判断当前版本。
	// 宁可整组只展示，也不把可能正在使用的版本标成可删。
	if r.linkOnly && !isSymlink(link) {
		s.reviewCopies(root, r, "入口不是符号链接，无法确认当前版本")
		return
	}
	if !isSymlink(link) {
		return
	}

	active, err := filepath.EvalSymlinks(link)
	if err != nil {
		s.errItem(link, r.Agent, err)
		return
	}
	var others []string
	for _, o := range r.OtherLinks {
		p := filepath.Join(s.home, o)
		if t, err := filepath.EvalSymlinks(p); err != nil || !isSymlink(p) || t != active {
			review("命令链接指向不同版本，不自动判断旧版")
			return
		}
		others = append(others, p)
	}
	fi, err := os.Stat(active)
	if err != nil {
		s.errItem(active, r.Agent, err)
		return
	}
	if r.Executable != "" {
		if exe, err := os.Stat(filepath.Join(active, r.Executable)); !fi.IsDir() || err != nil || !exe.Mode().IsRegular() {
			return
		}
	} else if !fi.Mode().IsRegular() {
		return
	}
	if filepath.Dir(active) != root {
		review("当前版本链接未指向版本目录，不自动判断旧版")
		return
	}

	type entry struct {
		path string
		v    version
	}
	var entries []entry
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		s.errItem(root, r.Agent, err)
		return
	}
	for _, e := range dirEntries {
		name := versionBase(e.Name())
		v, ok := parseVersion(name, r.Pattern)
		wantDir := r.Executable != ""
		if ok && e.Type()&os.ModeSymlink == 0 && e.IsDir() == wantDir {
			entries = append(entries, entry{filepath.Join(root, e.Name()), v})
		}
	}
	newest, found := -1, false
	for i, e := range entries {
		if e.path == active {
			found = true
		}
		if newest < 0 || compareVersion(e.v, entries[newest].v) > 0 {
			newest = i
		}
	}
	if !found {
		review("无法确认当前版本，不自动判断旧版")
		return
	}
	for i, e := range entries {
		if e.path != active && i != newest {
			s.add(e.path, root, r.Agent, CatOld, 0, link, others)
		}
	}
}

// versionBase 去掉版本文件名末尾的 .exe，让 2.1.3.exe 仍按 semver 解析。
// 只看名字本身：测试在 macOS 上模拟 Windows 布局时也要能剥掉。
func versionBase(name string) string {
	return strings.TrimSuffix(name, ".exe")
}

// reviewCopies 无法确认当前版本时，把版本目录里的候选都标成待核查
func (s *scanner) reviewCopies(root string, r versionRule, note string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		s.errItem(root, r.Agent, err)
		return
	}
	wantDir := r.Executable != ""
	var paths []string
	for _, e := range entries {
		if _, ok := parseVersion(versionBase(e.Name()), r.Pattern); !ok {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 || e.IsDir() != wantDir {
			continue
		}
		paths = append(paths, filepath.Join(root, e.Name()))
	}
	s.reviewAll(paths, r.Agent, note)
}
