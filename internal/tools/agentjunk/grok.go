package agentjunk

import (
	"os"
	"path/filepath"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
)

// scanGrok 只保留 grok 命令链接指向的版本文件。
// 安装器有时把版本放在 bin，有时放在 downloads，还会另建一个可能指向旧版的 agent 链接；
// 除当前版本外，两处的其他版本和 agent 链接都作为旧版本候选。
func (s *scanner) scanGrok(progress func(string)) {
	bin := filepath.Join(s.home, grokBin)
	link := filepath.Join(bin, exe("grok"))
	if !plainDir(bin) {
		return
	}
	if _, err := os.Lstat(link); err != nil {
		return
	}
	progress("检查 Grok 旧版本")
	var dirs []string
	for _, rel := range []string{grokBin, grokDownloads} {
		if d := filepath.Join(s.home, rel); plainDir(d) {
			dirs = append(dirs, d)
		}
	}
	if !isSymlink(link) {
		// Windows 原生安装把入口复制成普通 exe，无法确认当前版本，只展示不自动勾选
		if goos == "windows" {
			for _, d := range dirs {
				s.reviewAll(grokVersions(d), "Grok", "入口不是符号链接，无法确认当前版本")
			}
		}
		return
	}
	active, err := filepath.EvalSymlinks(link)
	if err != nil {
		s.errItem(link, "Grok", err)
		return
	}
	if fi, err := os.Stat(active); err != nil || !fi.Mode().IsRegular() || !inDirs(active, dirs) {
		s.items = append(s.items, cleanup.Item{
			Path: link, Name: filepath.Base(link), Group: "Grok", Category: CatReview,
			Note: "grok 链接未指向 bin 或 downloads 下的版本文件，不自动判断旧版",
		})
		return
	}

	base := ref{activeLink: link, activeDirs: dirs}
	for _, d := range dirs {
		for _, p := range grokVersions(d) {
			if samePath(p, active) {
				continue
			}
			n := len(s.items)
			s.add(p, d, "Grok", CatOld, 0, base)
			if len(s.items) > n && s.items[n].Category == CatOld {
				s.items[n].Note = "不是 grok 命令当前指向的版本"
			}
		}
	}

	// agent 是安装器额外创建的命令链接，只删链接本身，不影响 grok
	for _, agent := range s.grokAgentLinks(bin) {
		id, err := identify(agent)
		if err != nil {
			s.errItem(agent, "Grok", err)
			continue
		}
		it := cleanup.Item{
			Path: agent, Name: filepath.Base(agent), Group: "Grok", Category: CatOld,
			Selectable: true, Selected: true, Note: "Grok 附带的 agent 命令链接，使用 grok 命令即可",
		}
		if fi, err := os.Lstat(agent); err == nil {
			it.Size, it.Sized = fsx.AllocSize(fi), true
		}
		r := base
		r.root, r.id, r.link = filepath.Dir(agent), id, true
		it.Ref = r
		s.items = append(s.items, it)
	}
}

// grokAgentLinks 找出安装器创建的 agent 链接。~/.grok/bin 下的直接认定；
// ~/.grok/bin 不在 PATH 时安装器还会在 ~/.local/bin 或 /usr/local/bin 再建一份，
// 这两处的 agent 可能属于其他工具，只认指向 ~/.grok 内的链接（目标已删除的悬空链接也算）。
func (s *scanner) grokAgentLinks(bin string) []string {
	var out []string
	if p := filepath.Join(bin, exe("agent")); isSymlink(p) {
		out = append(out, p)
	}
	if goos == "windows" {
		return out
	}
	grokHome := filepath.Join(s.home, ".grok")
	for _, dir := range []string{filepath.Join(s.home, ".local/bin"), grokSystemBin} {
		p := filepath.Join(dir, "agent")
		if !plainDir(dir) || !isSymlink(p) {
			continue
		}
		target, err := os.Readlink(p)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if within(filepath.Clean(target), grokHome) {
			out = append(out, p)
		}
	}
	return out
}

// grokVersions 目录下符合 Grok 版本名的普通文件
func grokVersions(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if _, ok := parseVersion(versionBase(e.Name()), grokName); ok && e.Type().IsRegular() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}
