// Package obsidianlink 把一个工作目录按一级子目录选择性地链接进 Obsidian 库。
//
// ln -s 无法排除子目录，整目录链进库后归档、素材、node_modules 都会被索引。
// 这里把库内目标改成普通文件夹，只为勾选的一级子目录建立链接；源目录保持不变，可反复执行。
// macOS/Linux 使用符号链接；Windows 使用目录 junction（不需要管理员或开发者模式）。
package obsidianlink

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Options 源目录、库内目标与勾选记录位置
type Options struct {
	Src             string
	Dest            string
	DefaultExcludes []string // 没有任何记录时默认不勾选的目录
	StateFile       string   // 勾选记录
}

// State 库内目标的现状
type State int

const (
	StateMissing   State = iota // 不存在，将新建
	StateFullLink               // 整目录符号链接，将改为按子目录链接
	StateSelective              // 已是按子目录链接的普通文件夹
)

// Snapshot 当前状况与默认勾选
type Snapshot struct {
	State    State
	Dirs     []string        // 源目录下现有的一级子目录（不含隐藏目录）
	Linked   map[string]bool // 库内已链接到源目录的子目录
	Checked  map[string]bool // 默认勾选
	Vanished []string        // 记录里有、但源目录里已不存在的目录
}

// Load 检查路径并读取现状
func Load(o Options) (Snapshot, error) {
	var s Snapshot
	if fi, err := os.Stat(o.Src); err != nil || !fi.IsDir() {
		return s, fmt.Errorf("源目录不存在：%s", o.Src)
	}
	if _, err := os.Stat(filepath.Dir(o.Dest)); err != nil {
		return s, fmt.Errorf("库内父目录不存在：%s", filepath.Dir(o.Dest))
	}
	state, err := detect(o.Dest)
	if err != nil {
		return s, err
	}
	s.State = state
	if s.Dirs, err = listDirs(o.Src); err != nil {
		return s, err
	}
	if len(s.Dirs) == 0 {
		return s, fmt.Errorf("源目录下没有可链接的子文件夹：%s", o.Src)
	}
	s.Linked = linked(o)
	remembered := loadRemembered(o)
	exists := toSet(s.Dirs)
	for _, n := range remembered {
		if !exists[n] {
			s.Vanished = append(s.Vanished, n)
		}
	}
	s.Checked = initialChecked(s.Dirs, s.Linked, remembered, o.DefaultExcludes)
	return s, nil
}

func detect(dest string) (State, error) {
	fi, err := os.Lstat(dest)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return StateMissing, nil
	case err != nil:
		return 0, err
	case isLink(fi):
		return StateFullLink, nil
	case fi.IsDir():
		return StateSelective, nil
	}
	return 0, fmt.Errorf("目标路径存在但既不是目录也不是链接，请先手工检查：%s", dest)
}

func listDirs(src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") && e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// linked 库内指向源目录同名子目录的链接
func linked(o Options) map[string]bool {
	m := map[string]bool{}
	fi, err := os.Lstat(o.Dest)
	if err != nil || !fi.IsDir() {
		return m
	}
	entries, _ := os.ReadDir(o.Dest)
	for _, e := range entries {
		child := filepath.Join(o.Dest, e.Name())
		// ReadDir 的 Type 在 Windows junction 上不含 ModeSymlink，必须 Lstat
		fi, err := os.Lstat(child)
		if err != nil || !isLink(fi) {
			continue
		}
		target, err := os.Readlink(child)
		if err == nil && sameLinkTarget(target, filepath.Join(o.Src, e.Name()), e.Name()) {
			m[e.Name()] = true
		}
	}
	return m
}

// initialChecked 默认勾选：优先用上次的记录，其次用现有链接，最后按默认排除规则
func initialChecked(dirs []string, linked map[string]bool, remembered, excludes []string) map[string]bool {
	exists := toSet(dirs)
	checked := map[string]bool{}
	switch {
	case len(remembered) > 0:
		for _, n := range remembered {
			if exists[n] {
				checked[n] = true
			}
		}
	case len(linked) > 0:
		for n := range linked {
			if exists[n] {
				checked[n] = true
			}
		}
	default:
		ex := toSet(excludes)
		for _, n := range dirs {
			if !ex[n] {
				checked[n] = true
			}
		}
	}
	return checked
}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Plan 需要执行的变更
type Plan struct {
	Keep   []string // 已链接且保持
	Add    []string // 新增链接
	Remove []string // 移除链接
	Skip   []string // 库内是真实路径，不会删除
}

// Empty 没有任何链接变更
func (p Plan) Empty() bool { return len(p.Add) == 0 && len(p.Remove) == 0 }

// BuildPlan 根据勾选计算变更
func BuildPlan(o Options, s Snapshot, chosen map[string]bool) Plan {
	var p Plan
	for _, n := range s.Dirs {
		child := filepath.Join(o.Dest, n)
		switch {
		case chosen[n] && s.Linked[n]:
			p.Keep = append(p.Keep, n)
		case chosen[n]:
			p.Add = append(p.Add, n)
		case isLinkPath(child):
			p.Remove = append(p.Remove, n)
		case exists(child) && s.State == StateSelective:
			p.Skip = append(p.Skip, n)
		}
	}
	// 源目录里已经消失的子目录，其残留链接也一并移除
	if s.State == StateSelective {
		dirs := toSet(s.Dirs)
		var gone []string
		for n := range s.Linked {
			if !dirs[n] {
				gone = append(gone, n)
			}
		}
		sort.Strings(gone)
		p.Remove = append(p.Remove, gone...)
	}
	return p
}

// Apply 执行变更并保存勾选记录
func Apply(o Options, s Snapshot, p Plan, chosen []string) []sysx.Step {
	rec := &sysx.Recorder{}
	switch s.State {
	case StateFullLink:
		err := os.Remove(o.Dest)
		if err == nil {
			err = os.MkdirAll(o.Dest, 0o755)
		}
		rec.Note("整目录链接改为普通文件夹", err)
		if err != nil {
			return rec.Steps
		}
	case StateMissing:
		err := os.MkdirAll(o.Dest, 0o755)
		rec.Note("创建目标文件夹", err)
		if err != nil {
			return rec.Steps
		}
	}
	for _, n := range p.Add {
		child := filepath.Join(o.Dest, n)
		if exists(child) && !isLinkPath(child) {
			rec.Note("跳过 "+n+"：库内已有真实路径", nil)
			continue
		}
		_ = os.Remove(child) // 指向别处的旧链接先移除；Remove 只删链接本身
		rec.Note("链接 "+n, linkDir(filepath.Join(o.Src, n), child))
	}
	for _, n := range p.Remove {
		child := filepath.Join(o.Dest, n)
		if !isLinkPath(child) {
			rec.Note("跳过 "+n+"：不是链接", nil)
			continue
		}
		// 必须用 Remove：junction 上 RemoveAll 会顺着链接删掉源目录内容
		rec.Note("移除 "+n, os.Remove(child))
	}
	rec.Note("保存勾选记录", SaveRemembered(o, chosen))
	return rec.Steps
}

// isLinkPath 路径本身是否为符号链接或 Windows 目录 junction。
// junction 在 Go 1.23+ 的 Lstat 里是 ModeIrregular，不是 ModeSymlink，不能只看后者。
func isLinkPath(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && isLink(fi)
}

// sameLinkTarget 链接目标是否指向期望路径。
// Windows 的 Readlink 用反斜杠，且可能带 \\?\ 前缀；比较时统一成当前平台的分隔符。
func sameLinkTarget(target, abs, name string) bool {
	if target == name {
		return true
	}
	return filepath.Clean(normalizeLink(target)) == filepath.Clean(abs)
}

func normalizeLink(target string) string {
	const prefix = `\\?\`
	if len(target) >= len(prefix) && target[:len(prefix)] == prefix {
		target = target[len(prefix):]
	}
	if os.PathSeparator == '\\' {
		return strings.ReplaceAll(target, "/", `\`)
	}
	return strings.ReplaceAll(target, `\`, "/")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

const stateHeader = "# sysbox obsidian 链接勾选记录，删掉本文件则下次按默认规则预勾"

// loadRemembered 读取勾选记录，没有记录时返回空
func loadRemembered(o Options) []string {
	names, _ := readNames(o.StateFile)
	return names
}

func readNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			names = append(names, line)
		}
	}
	return names, sc.Err()
}

// SaveRemembered 写入勾选记录
func SaveRemembered(o Options, names []string) error {
	if err := os.MkdirAll(filepath.Dir(o.StateFile), 0o755); err != nil {
		return err
	}
	content := stateHeader + "\n" + strings.Join(names, "\n") + "\n"
	return os.WriteFile(o.StateFile, []byte(content), 0o644)
}
