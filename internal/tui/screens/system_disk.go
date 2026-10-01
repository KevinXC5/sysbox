package screens

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/tools/disk"
)

// diskState 缓存最近一次扫描的目录树，进入子目录、返回上级与切换视图都直接复用
type diskState struct {
	mu       sync.Mutex
	tree     *disk.Tree
	progress *disk.Progress
	home     string
}

func (s *diskState) short(path string) string {
	if rel, err := filepath.Rel(s.home, path); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return "~"
		}
		return filepath.Join("~", rel)
	}
	return path
}

func NewDisk(env Env) Page {
	home, err := os.UserHomeDir()
	if err != nil {
		return NewErrorPage([]string{"系统", "磁盘分析"}, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return NewErrorPage([]string{"系统", "磁盘分析"}, err)
	}
	s := &diskState{home: home}
	spec := systemSpec{name: "磁盘分析", tabs: []string{"目录占用", "大文件", "磁盘容量"}, target: cwd}
	spec.columns = func(tab int) []systemColumn {
		switch tab {
		case 0:
			return []systemColumn{{title: "名称"}, {title: "大小", width: 9, right: true}}
		case 1:
			return []systemColumn{{title: "文件"}, {title: "大小", width: 9, right: true}, {title: "修改日期", width: 10}, {title: "所在目录"}}
		}
		return []systemColumn{{title: "位置"}, {title: "占用", width: 14, bar: true}, {title: "已用", width: 9, right: true}, {title: "总量", width: 9, right: true}, {title: "可用", width: 9, right: true}}
	}
	spec.input = func(int) string { return "扫描目录" }
	spec.submit = func(tab int, value string) (int, string) {
		if tab == 2 {
			tab = 0
		}
		if path, err := disk.Expand(value); err == nil {
			value = path
		}
		return tab, value
	}
	spec.parent = func(tab int, target string) (string, bool) {
		if tab == 2 {
			return "", false
		}
		path, err := disk.Expand(target)
		if err != nil || filepath.Dir(path) == path {
			return "", false
		}
		return filepath.Dir(path), true
	}
	spec.load = func(ctx context.Context, tab int, target string, force bool) (systemResult, error) {
		if tab == 2 {
			return s.loadVolumes(ctx)
		}
		return s.loadTree(ctx, tab, target, force)
	}
	spec.live = func(tab int) (systemResult, bool) {
		s.mu.Lock()
		progress := s.progress
		s.mu.Unlock()
		if tab == 2 || progress == nil {
			return systemResult{}, false
		}
		files, bytes, current := progress.Snapshot()
		return systemResult{info: fmt.Sprintf("正在扫描 · 已统计 %d 个文件 · %s · %s", files, fsx.FormatBytes(bytes), s.short(current)), warn: "大目录可能需要一些时间；按 esc 停止扫描并查看已统计的部分"}, true
	}
	spec.actions = func(tab int, row systemRow) []systemAction {
		node, ok := row.value.(*disk.Node)
		if !ok {
			return nil
		}
		reveal := sysx.C("open", "-R", node.Path)
		revealLabel, trashLabel := "在访达中显示", "移到废纸篓"
		if runtime.GOOS == "windows" {
			reveal = sysx.C("explorer.exe", "/select,"+node.Path)
			revealLabel, trashLabel = "在资源管理器中显示", "移到回收站"
		}
		actions := []systemAction{{key: "o", label: revealLabel, quiet: true, run: func(ctx context.Context) (string, error) {
			_, err := (sysx.ExecRunner{}).Run(ctx, reveal)
			return "", err
		}}}
		if disk.Trashable(node.Path) == nil {
			kind := "文件"
			if node.Dir {
				kind = fmt.Sprintf("目录（%d 个文件）", node.Files)
			}
			actions = append(actions, systemAction{key: "d", label: trashLabel, mutates: true, preview: node.Path, note: fmt.Sprintf("%s · %s，可从%s中还原", kind, fsx.FormatBytes(node.Bytes), strings.TrimPrefix(trashLabel, "移到")), run: func(ctx context.Context) (string, error) {
				if err := disk.Trash(ctx, node.Path); err != nil {
					return "", err
				}
				s.mu.Lock()
				tree := s.tree
				s.mu.Unlock()
				if tree != nil {
					tree.Remove(node.Path)
				}
				return fmt.Sprintf("已%s：%s，释放 %s", trashLabel, node.Name, fsx.FormatBytes(node.Bytes)), nil
			}})
		}
		if id := disk.CleanupTool(node.Path); id != "" {
			actions = append(actions, systemAction{key: "c", label: "打开清理工具", tool: id})
		}
		return actions
	}
	p := &diskPage{systemPage: newSystemPage(env, spec), state: s}
	p.spec.panes = p.panes
	return p
}

func (s *diskState) loadVolumes(ctx context.Context) (systemResult, error) {
	volumes, err := disk.Volumes(ctx)
	if err != nil {
		return systemResult{}, err
	}
	result := systemResult{info: "选择磁盘或常用目录，按 enter 扫描；也可按 g 输入任意目录"}
	for _, v := range volumes {
		used := v.Total - v.Free
		ratio := float64(used) / float64(max(int64(1), v.Total))
		name := v.Name
		if v.Name != v.Path {
			name += " " + v.Path
		}
		result.rows = append(result.rows, systemRow{name: name, key: v.Path, cells: []string{"", fsx.FormatBytes(used), fsx.FormatBytes(v.Total), fsx.FormatBytes(v.Free)}, ratio: ratio, target: v.Path, fields: []containers.Field{{Value: "磁盘"}, {Label: "名称", Value: v.Name}, {Label: "挂载路径", Value: v.Path}, {Label: "总容量", Value: fsx.FormatBytes(v.Total)}, {Label: "已用", Value: fmt.Sprintf("%s（%.0f%%）", fsx.FormatBytes(used), ratio*100)}, {Label: "可用", Value: fsx.FormatBytes(v.Free)}}})
	}
	places := []struct{ name, path string }{{"家目录", s.home}, {"下载", filepath.Join(s.home, "Downloads")}, {"桌面", filepath.Join(s.home, "Desktop")}, {"文稿", filepath.Join(s.home, "Documents")}, {"应用缓存", filepath.Join(s.home, "Library", "Caches")}}
	if runtime.GOOS == "windows" {
		places[4] = struct{ name, path string }{"应用数据", filepath.Join(s.home, "AppData", "Local")}
	}
	for _, place := range places {
		if info, err := os.Stat(place.path); err != nil || !info.IsDir() {
			continue
		}
		result.rows = append(result.rows, systemRow{name: place.name + " " + s.short(place.path), key: place.path, cells: []string{"", "", "", ""}, ratio: -1, target: place.path, fields: []containers.Field{{Value: "常用目录"}, {Label: "路径", Value: place.path}, {Label: "说明", Value: "按 enter 扫描此目录"}}})
	}
	return result, nil
}

// loadTree 目标在已扫描的目录树内时直接展示；否则重新扫描，返回上级时复用当前目录树
func (s *diskState) loadTree(ctx context.Context, tab int, target string, force bool) (systemResult, error) {
	path, err := disk.Expand(target)
	if err != nil {
		return systemResult{}, err
	}
	s.mu.Lock()
	tree := s.tree
	s.mu.Unlock()
	if force || tree == nil || tree.Find(path) == nil {
		var reuse *disk.Tree
		if !force && tree != nil && disk.Within(path, tree.Root.Path) {
			reuse = tree
		}
		progress := &disk.Progress{}
		s.mu.Lock()
		s.progress = progress
		s.mu.Unlock()
		scanned, err := disk.Scan(ctx, path, progress, reuse)
		s.mu.Lock()
		s.progress = nil
		if scanned != nil {
			s.tree = scanned
		}
		s.mu.Unlock()
		if scanned == nil {
			return systemResult{}, err
		}
		tree = scanned
	}
	node := tree.Find(path)
	if node == nil {
		return systemResult{}, fmt.Errorf("目录不在扫描范围内")
	}
	result := systemResult{info: fmt.Sprintf("%s · 合计 %s · %d 个文件 · 扫描于 %s", s.short(node.Path), fsx.FormatBytes(node.Bytes), node.Files, tree.Scanned.Format("15:04"))}
	var warns []string
	if tree.Partial {
		warns = append(warns, "扫描已停止，结果不完整；按 r 重新扫描")
	}
	if tree.Skipped > 0 {
		warns = append(warns, fmt.Sprintf("%d 个目录无权限读取，未计入", tree.Skipped))
	}
	result.warn = strings.Join(warns, " · ")
	if tab == 1 {
		for _, n := range tree.LargestUnder(node.Path) {
			dir, _ := filepath.Rel(node.Path, filepath.Dir(n.Path))
			result.rows = append(result.rows, systemRow{name: n.Name, key: n.Path, cells: []string{fsx.FormatBytes(n.Bytes), n.Modified.Format("2006-01-02"), dir}, ratio: -1, value: n, fields: s.nodeFields(n, node)})
		}
		return result, nil
	}
	for _, child := range node.Children {
		row := systemRow{name: child.Name, key: child.Path, cells: []string{fsx.FormatBytes(child.Bytes)}, ratio: float64(child.Bytes) / float64(max(1, node.Bytes)), value: child, fields: s.nodeFields(child, node)}
		if child.Dir {
			row.name += string(filepath.Separator)
			row.target = child.Path
		}
		result.rows = append(result.rows, row)
	}
	if node.Rest > 0 {
		result.rows = append(result.rows, systemRow{name: fmt.Sprintf("其他 %d 个小文件", node.Rest), key: "rest", cells: []string{fsx.FormatBytes(node.RestBytes)}, ratio: float64(node.RestBytes) / float64(max(1, node.Bytes)), fields: []containers.Field{{Value: "合并统计"}, {Label: "说明", Value: "文件较多的目录只单独列出较大的文件，其余合并为一行；可在「大文件」中查看"}, {Label: "数量", Value: fmt.Sprint(node.Rest)}, {Label: "占用", Value: fsx.FormatBytes(node.RestBytes)}}})
	}
	return result, nil
}

func (s *diskState) nodeFields(n, parent *disk.Node) []containers.Field {
	kind := "文件"
	if n.Dir {
		kind = "目录"
	}
	fields := []containers.Field{{Value: "基本信息"}, {Label: "路径", Value: n.Path}, {Label: "类型", Value: kind}, {Label: "占用", Value: fmt.Sprintf("%s（占 %s 的 %.1f%%）", fsx.FormatBytes(n.Bytes), s.short(parent.Path), float64(n.Bytes)/float64(max(1, parent.Bytes))*100)}}
	if n.Dir {
		fields = append(fields, containers.Field{Label: "文件数", Value: fmt.Sprint(n.Files)})
	}
	fields = append(fields, containers.Field{Label: "修改时间", Value: n.Modified.Format("2006-01-02 15:04")})
	if id := disk.CleanupTool(n.Path); id != "" {
		fields = append(fields, containers.Field{Value: "清理"}, containers.Field{Label: "建议", Value: "属于已识别的缓存或构建产物，按 c 打开对应清理工具，由其按规则安全清理"})
	}
	if err := disk.Trashable(n.Path); err != nil {
		fields = append(fields, containers.Field{Label: "删除", Value: err.Error()})
	}
	if runtime.GOOS != "windows" && n.Dir {
		fields = append(fields, containers.Field{Value: "统计说明"}, containers.Field{Label: "口径", Value: "按实际分配块统计，不跟随符号链接；APFS 克隆文件共享的空间可能重复计入"})
	}
	return fields
}
