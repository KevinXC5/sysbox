package jetbrains

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// acp-agents 目录结构：<agent>/<版本>/ 为各版本本体，.downloads/<agent>/<版本>/ 为下载包，
// IDE 升级 agent 后不会删除旧版本，因此每个 agent 只保留最新版本
const (
	acpAgentsName = "acp-agents"
	acpDownloads  = ".downloads"
)

// acpNotes acp-agents 下需要整体保留的条目
var acpNotes = map[string]string{
	"registry.json": "Agent 注册表，删除后需要重新下载",
	".runtimes":     "Agent 共享运行时，删除后需要重新下载",
}

var semverName = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// classifyAgents 展开 acp-agents 目录，旧版本可清理，最新版本与其余条目跳过
func classifyAgents(p, group string) []cleanup.Item {
	whole := with(cleanup.Item{Path: p, Name: acpAgentsName, Group: group, IsDir: true}, CatSkip, skipRules[acpAgentsName])
	entries, err := os.ReadDir(p)
	if err != nil {
		return []cleanup.Item{whole}
	}

	var items []cleanup.Item
	for _, e := range entries {
		ep := filepath.Join(p, e.Name())
		name := acpAgentsName + "/" + e.Name()
		it := cleanup.Item{Path: ep, Name: name, Group: group, IsDir: e.IsDir()}
		isLink := e.Type()&fs.ModeSymlink != 0

		switch {
		case isLink:
			items = append(items, with(it, CatLeftover, noteSymlink))
		case e.Name() == acpDownloads && e.IsDir():
			// 下载包同样按 agent 分目录，逐个只保留最新版本
			subs, err := os.ReadDir(ep)
			if err != nil {
				items = append(items, with(it, CatLeftover, noteUnread))
				continue
			}
			for _, s := range subs {
				sp := filepath.Join(ep, s.Name())
				sit := cleanup.Item{Path: sp, Name: name + "/" + s.Name(), Group: group, IsDir: s.IsDir()}
				if s.IsDir() && s.Type()&fs.ModeSymlink == 0 {
					items = append(items, agentVersions(sp, sit.Name, group, "下载包")...)
				} else {
					items = append(items, with(sit, CatLeftover, noteUnknown))
				}
			}
		case acpNotes[e.Name()] != "":
			items = append(items, with(it, CatSkip, acpNotes[e.Name()]))
		case e.IsDir() && !strings.HasPrefix(e.Name(), "."):
			items = append(items, agentVersions(ep, name, group, "Agent")...)
		default:
			items = append(items, with(it, CatLeftover, noteUnknown))
		}
	}
	return items
}

// agentVersions 对单个 agent 的版本目录分类：最新版本跳过，更旧的版本可清理，非版本条目只展示
func agentVersions(dir, prefix, group, kind string) []cleanup.Item {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []cleanup.Item{with(cleanup.Item{Path: dir, Name: prefix, Group: group, IsDir: true}, CatLeftover, noteUnread)}
	}

	type ver struct {
		it cleanup.Item
		v  version
	}
	var vers []ver
	var items []cleanup.Item
	for _, e := range entries {
		it := cleanup.Item{Path: filepath.Join(dir, e.Name()), Name: prefix + "/" + e.Name(), Group: group, IsDir: e.IsDir()}
		v, ok := parseVersion(e.Name())
		if !ok || !e.IsDir() || e.Type()&fs.ModeSymlink != 0 {
			// .DS_Store 之类的杂项和无法识别的条目一律不动
			items = append(items, with(it, CatLeftover, noteUnknown))
			continue
		}
		vers = append(vers, ver{it, v})
	}

	newest := -1
	for i, x := range vers {
		if newest < 0 || compareVersion(x.v, vers[newest].v) > 0 {
			newest = i
		}
	}
	for i, x := range vers {
		if i == newest {
			items = append(items, with(x.it, CatSkip, kind+" 当前版本，删除后需要重新下载"))
		} else {
			latest := filepath.Base(vers[newest].it.Path)
			items = append(items, with(x.it, CatClean, kind+" 旧版本，已有 "+latest))
		}
	}
	return items
}

// version 语义化版本号：正式版高于同号预发布版
type version struct {
	nums [3]int
	pre  []string
}

// parseVersion 解析 x.y.z[-pre][+build] 形式的目录名
func parseVersion(name string) (v version, ok bool) {
	m := semverName.FindStringSubmatch(name)
	if m == nil {
		return v, false
	}
	for i := range 3 {
		v.nums[i], _ = strconv.Atoi(m[1+i])
	}
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
	}
	return v, true
}

// compareVersion 返回 -1、0、1，预发布标识按语义化版本规则逐段比较
func compareVersion(a, b version) int {
	for i := range 3 {
		if c := cmp.Compare(a.nums[i], b.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		na, errA := strconv.Atoi(a.pre[i])
		nb, errB := strconv.Atoi(b.pre[i])
		var c int
		switch {
		case errA == nil && errB == nil:
			c = cmp.Compare(na, nb)
		case errA == nil:
			c = -1
		case errB == nil:
			c = 1
		default:
			c = cmp.Compare(a.pre[i], b.pre[i])
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.pre), len(b.pre))
}
