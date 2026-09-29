// Package projects 在项目目录下查找 node_modules、target、build 等构建产物。
// 超过保留期未活动的项目默认勾选，近期项目可手动勾选；产物都能按项目配置重新生成。
package projects

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
)

// DefaultKeepDays 项目多少天未活动算过期
const DefaultKeepDays = 30

// 分类下标，与 categories 顺序一致
const (
	CatStale  = iota // 可清理：项目超过保留期未活动
	CatRecent        // 近期项目：保留期内有修改
	CatSkip          // 跳过：无法读取
)

var categories = []cleanup.Category{
	CatStale:  {Label: "可清理", Tone: cleanup.ToneGood, Primary: true},
	CatRecent: {Label: "近期项目", Tone: cleanup.ToneInfo},
	CatSkip:   {Label: "跳过", Sub: "无法读取", Tone: cleanup.ToneMuted},
}

// defaultRoots 未配置时自动查找的项目目录，按家目录下的相对路径
var defaultRoots = []string{
	"Github", "GitHub", "github", "Projects", "projects", "Developer", "Code", "code",
	"src", "workspace", "Workspace", "dev", "repos",
	"IdeaProjects", "WebstormProjects", "PycharmProjects", "GolandProjects",
	filepath.Join("source", "repos"),
}

// Source 项目构建产物清理数据源
type Source struct {
	Dirs     []string // 项目根目录，已展开为绝对路径
	KeepDays int
	Now      func() time.Time
}

// NewSource 按配置确定项目根目录；roots 为空时自动查找常见目录
func NewSource(roots []string, keepDays int) (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if keepDays <= 0 {
		keepDays = DefaultKeepDays
	}
	rs := resolveRoots(home, roots)
	if len(rs) == 0 {
		if len(roots) > 0 {
			return nil, errors.New("配置的项目目录都不存在：" + strings.Join(roots, "、"))
		}
		return nil, errors.New("没有找到项目目录，请在配置文件的 projects.roots 中指定")
	}
	return &Source{Dirs: rs, KeepDays: keepDays, Now: time.Now}, nil
}

// resolveRoots 展开 ~、解析链接并去重；家目录和不存在的目录不作为根
func resolveRoots(home string, roots []string) []string {
	auto := len(roots) == 0
	if auto {
		for _, r := range defaultRoots {
			roots = append(roots, filepath.Join(home, r))
		}
	}
	realHome, _ := filepath.EvalSymlinks(home)
	var out []string
	var infos []os.FileInfo
	for _, r := range roots {
		if r == "~" || strings.HasPrefix(r, "~/") || strings.HasPrefix(r, `~\`) {
			r = filepath.Join(home, r[1:])
		}
		if !filepath.IsAbs(r) {
			continue
		}
		real, err := filepath.EvalSymlinks(r)
		if err != nil {
			continue
		}
		fi, err := os.Stat(real)
		// 整个家目录太大，也会扫进系统目录，只接受它下面的具体目录
		if err != nil || !fi.IsDir() || real == realHome || real == filepath.Dir(real) {
			continue
		}
		dup := false
		for _, prev := range infos {
			// macOS 默认不区分大小写，Github 与 GitHub 是同一个目录
			if os.SameFile(prev, fi) {
				dup = true
				break
			}
		}
		if !dup {
			infos = append(infos, fi)
			out = append(out, real)
		}
	}
	return out
}

// ref 扫描时记下的复核信息
type ref struct {
	id   fsx.FileID
	root string
}

var _ cleanup.Source = (*Source)(nil)

func (s *Source) Title() string { return "项目构建产物" }

func (s *Source) Categories() []cleanup.Category {
	cats := append([]cleanup.Category(nil), categories...)
	cats[CatStale].Sub = fmt.Sprintf("%d 天未活动", s.KeepDays)
	cats[CatRecent].Sub = fmt.Sprintf("%d 天内有修改", s.KeepDays)
	return cats
}

func (s *Source) Scan(progress func(string)) ([]cleanup.Item, error) {
	return s.ScanContext(context.Background(), progress)
}

// ScanContext 扫描可随调用方取消。
func (s *Source) ScanContext(ctx context.Context, progress func(string)) ([]cleanup.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := s.Now()
	keep := time.Duration(s.KeepDays) * 24 * time.Hour
	var items []cleanup.Item
	for _, root := range s.Dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if progress != nil {
			progress("扫描 " + fsx.PrettyPath(root))
		}
		for _, f := range walkContext(ctx, root, now) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			items = append(items, s.toItem(f, now, keep))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Source) toItem(f found, now time.Time, keep time.Duration) cleanup.Item {
	it := cleanup.Item{
		Path: f.path, Name: f.rule.dir, Group: group(f), IsDir: true, Selectable: true,
	}
	id, err := fsx.Identify(f.path)
	if err != nil {
		it.Category, it.Selectable, it.Sized = CatSkip, false, true
		it.Note = f.rule.kind + " 项目，无法读取文件标识（不可选）"
		return it
	}
	it.Ref = ref{id: id, root: f.root}
	days := int(now.Sub(f.active).Hours() / 24)
	if now.Sub(f.active) >= keep {
		it.Category = CatStale
		it.Selected = !f.rule.manual
		it.Note = fmt.Sprintf("%s 项目，%d 天未活动", f.rule.kind, days)
	} else {
		it.Category = CatRecent
		it.Note = fmt.Sprintf("%s 项目，%s有修改", f.rule.kind, ago(days))
	}
	if f.rule.manual {
		it.Irreversible = true
		it.Note += "；虚拟环境里手动安装的包需要重新安装"
	}
	return it
}

// group 界面上的所属列：项目根目录名加项目相对路径
func group(f found) string {
	rel, err := filepath.Rel(f.root, f.project)
	if err != nil || rel == "." {
		return filepath.Base(f.root)
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(f.root), rel))
}

func ago(days int) string {
	if days <= 0 {
		return "今天"
	}
	return fmt.Sprintf("%d 天前", days)
}

// Roots 项目目录可能很大，完成页不统计前后大小
func (s *Source) Roots() []cleanup.Root { return nil }

// Check 产物都能重新生成，不做运行检查
func (s *Source) Check() *cleanup.Notice { return nil }

// Remove 删除前复核：仍在所属项目根之下、不是链接、标识未变、所在目录仍是对应类型的项目
func (s *Source) Remove(it cleanup.Item) error {
	r, ok := it.Ref.(ref)
	if !ok || !it.Selectable {
		return errors.New("条目缺少复核信息，拒绝删除")
	}
	p := filepath.Clean(it.Path)
	if !s.isRoot(r.root) || !within(p, r.root) {
		return fmt.Errorf("拒绝删除项目目录之外的路径：%s", p)
	}
	// 各级父目录都不能是链接，否则真实位置可能已经不在项目根下
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || !samePath(parent, filepath.Dir(p)) {
		return errors.New("所在目录已变化，跳过")
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return errors.New("目标已变为链接或文件，跳过")
	}
	if id, err := fsx.Identify(p); err != nil || id != r.id {
		return errors.New("目录已被替换，跳过")
	}
	if _, ok := match(filepath.Dir(p), filepath.Base(p)); !ok {
		return errors.New("所在目录已不是对应类型的项目，跳过")
	}
	return fsx.ForceRemoveAll(p)
}

func (s *Source) isRoot(root string) bool {
	for _, r := range s.Dirs {
		if samePath(r, root) {
			return true
		}
	}
	return false
}

func (s *Source) Notes() []string {
	var roots []string
	for _, r := range s.Dirs {
		roots = append(roots, fsx.PrettyPath(r))
	}
	return []string{
		"扫描目录：" + strings.Join(roots, "、") + "，可在配置文件的 projects.roots 中修改",
		fmt.Sprintf("默认勾选 %d 天未活动项目的构建产物；Python 虚拟环境默认不勾选", s.KeepDays),
		"产物目录旁必须有对应的项目文件（如 package.json、Cargo.toml）才会列出",
	}
}

func (s *Source) Tip() string {
	return "再次开发时重新安装依赖或构建即可恢复"
}

// within p 位于 root 之下（不含 root 本身）
func within(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// samePath Windows 忽略大小写
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
