package projects

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

var now = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// project 在 root 下建一个项目：files 是项目文件，dirs 是产物目录，全部修改时间设为 age 之前
func project(t *testing.T, dir string, age time.Duration, files []string, dirs ...string) {
	t.Helper()
	for _, f := range files {
		write(t, filepath.Join(dir, f))
	}
	for _, d := range dirs {
		write(t, filepath.Join(dir, d, "payload"))
	}
	stamp := now.Add(-age)
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chtimes(p, stamp, stamp)
		}
		return nil
	})
}

func source(t *testing.T) (*Source, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Source{Dirs: []string{root}, KeepDays: 30, Now: func() time.Time { return now }}, root
}

func scanAll(t *testing.T, s *Source) map[string]cleanup.Item {
	t.Helper()
	items, err := s.Scan(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]cleanup.Item{}
	for _, it := range items {
		rel, _ := filepath.Rel(s.Dirs[0], it.Path)
		out[filepath.ToSlash(rel)] = it
	}
	return out
}

func TestClassifyByActivity(t *testing.T) {
	s, root := source(t)
	day := 24 * time.Hour
	project(t, filepath.Join(root, "old-web"), 90*day, []string{"package.json", "src/index.ts"}, "node_modules", ".next")
	project(t, filepath.Join(root, "new-web"), 2*day, []string{"package.json"}, "node_modules")
	project(t, filepath.Join(root, "group", "rusty"), 60*day, []string{"Cargo.toml"}, "target")
	project(t, filepath.Join(root, "old-py"), 90*day, []string{"pyproject.toml", ".venv/pyvenv.cfg"})
	// 没有项目标记的同名目录不能当成产物
	project(t, filepath.Join(root, "notes"), 90*day, []string{"readme.md"}, "target", "build")
	// 源码深处最近有修改的项目仍算活跃
	project(t, filepath.Join(root, "deep"), 90*day, []string{"pom.xml"}, "target")
	write(t, filepath.Join(root, "deep", "src", "main", "App.java"))
	_ = os.Chtimes(filepath.Join(root, "deep", "src", "main", "App.java"), now, now)

	got := scanAll(t, s)
	for _, k := range []string{"old-web/node_modules", "old-web/.next", "group/rusty/target"} {
		if it := got[k]; it.Category != CatStale || !it.Selected {
			t.Errorf("%s 应默认勾选：%+v", k, it)
		}
	}
	if it := got["new-web/node_modules"]; it.Category != CatRecent || it.Selected || !it.Selectable {
		t.Errorf("近期项目应可选但默认不勾选：%+v", it)
	}
	if it := got["deep/target"]; it.Category != CatRecent {
		t.Errorf("源码刚修改过的项目应算近期：%+v", it)
	}
	if it := got["old-py/.venv"]; it.Category != CatStale || it.Selected || !it.Irreversible {
		t.Errorf("虚拟环境应默认不勾选并警示：%+v", it)
	}
	for _, k := range []string{"notes/target", "notes/build"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s 没有项目标记，不应列出", k)
		}
	}
	if g := got["group/rusty/target"].Group; g != filepath.Base(root)+"/group/rusty" {
		t.Errorf("所属列应是根目录名加相对路径，实际 %s", g)
	}
}

func TestNestedNodeModulesNotListed(t *testing.T) {
	s, root := source(t)
	project(t, filepath.Join(root, "app"), 90*24*time.Hour, []string{"package.json", "node_modules/dep/package.json"}, "node_modules/dep/node_modules")
	got := scanAll(t, s)
	if _, ok := got["app/node_modules"]; !ok {
		t.Fatal("应列出项目的 node_modules")
	}
	for k := range got {
		if strings.Count(k, "node_modules") > 1 {
			t.Fatalf("不应深入 node_modules 内部：%s", k)
		}
	}
}

func TestSymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("创建符号链接需要额外权限")
	}
	s, root := source(t)
	outside := t.TempDir()
	project(t, outside, 90*24*time.Hour, []string{"package.json"}, "node_modules")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if len(scanAll(t, s)) != 0 {
		t.Fatal("不应跟随符号链接扫描项目根之外的目录")
	}
}

func TestRemoveRechecks(t *testing.T) {
	s, root := source(t)
	project(t, filepath.Join(root, "a"), 90*24*time.Hour, []string{"package.json"}, "node_modules")
	project(t, filepath.Join(root, "b"), 90*24*time.Hour, []string{"Cargo.toml"}, "target")
	project(t, filepath.Join(root, "c"), 90*24*time.Hour, []string{"package.json"}, "node_modules")
	got := scanAll(t, s)

	// 伪造成项目根之外的路径
	forged := got["a/node_modules"]
	forged.Path = filepath.Join(t.TempDir(), "node_modules")
	if err := s.Remove(forged); err == nil {
		t.Fatal("项目根之外的路径必须拒绝")
	}
	// 项目文件被删掉后不再是 Rust 项目
	if err := os.Remove(filepath.Join(root, "b", "Cargo.toml")); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(got["b/target"]); err == nil {
		t.Fatal("项目标记消失后必须跳过")
	}
	// 扫描后目录被替换
	c := got["c/node_modules"]
	if err := os.RemoveAll(c.Path); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(c.Path, "new"))
	if err := s.Remove(c); err == nil {
		t.Fatal("被替换的目录必须跳过")
	}
	// 正常删除
	if err := s.Remove(got["a/node_modules"]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "node_modules")); !os.IsNotExist(err) {
		t.Fatal("node_modules 应被删除")
	}
	if _, err := os.Stat(filepath.Join(root, "a", "package.json")); err != nil {
		t.Fatal("项目文件不能受影响")
	}
}

func TestResolveRoots(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Github"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 自动查找：只保留存在的常见目录；大小写不敏感的文件系统上 GitHub 与 Github 只算一个
	auto := resolveRoots(home, nil)
	if len(auto) != 1 || filepath.Base(auto[0]) != "Github" {
		t.Fatalf("自动查找结果不对：%v", auto)
	}
	// 家目录本身和不存在的目录不作为根，~ 开头的路径会展开
	got := resolveRoots(home, []string{"~", home, "~/Github", "~/missing", "relative"})
	if len(got) != 1 || got[0] != filepath.Join(home, "Github") {
		t.Fatalf("配置的根目录解析不对：%v", got)
	}
}

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
