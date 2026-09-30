package projects

import (
	"io/fs"
	"os"
	"os/exec"
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
	project(t, filepath.Join(root, "group", "rusty"), 60*day, []string{"Cargo.toml", "target/CACHEDIR.TAG"})
	project(t, filepath.Join(root, "old-py"), 90*day, []string{"pyproject.toml", ".venv/pyvenv.cfg"})
	// 没有项目标记的同名目录不能当成产物
	project(t, filepath.Join(root, "notes"), 90*day, []string{"readme.md"}, "target", "build")
	// 源码深处最近有修改的项目仍算活跃
	project(t, filepath.Join(root, "deep"), 90*day, []string{"pom.xml", "target/classes/App.class"})
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
	// 未配置时以整个家目录为根
	auto := resolveRoots(home, nil)
	if len(auto) != 1 || auto[0] != home {
		t.Fatalf("自动查找结果不对：%v", auto)
	}
	// 家目录本身和不存在的目录不作为根，~ 开头的路径会展开
	got := resolveRoots(home, []string{"~", home, "~/Github", "~/missing", "relative"})
	if len(got) != 1 || got[0] != filepath.Join(home, "Github") {
		t.Fatalf("配置的根目录解析不对：%v", got)
	}
}

func TestBuildDirNeedsToolTraits(t *testing.T) {
	root := t.TempDir()
	// Gradle：build 里有 classes、libs 等才算产物，只放脚本的 build 保留
	write(t, filepath.Join(root, "gradle-app", "build.gradle"))
	write(t, filepath.Join(root, "gradle-app", "build", "libs", "app.jar"))
	write(t, filepath.Join(root, "gradle-scripts", "build.gradle"))
	write(t, filepath.Join(root, "gradle-scripts", "build", "deploy.sh"))
	// Maven、Rust 的 target 同理
	write(t, filepath.Join(root, "mvn", "pom.xml"))
	write(t, filepath.Join(root, "mvn", "target", "notes.txt"))
	write(t, filepath.Join(root, "rs", "Cargo.toml"))
	write(t, filepath.Join(root, "rs", "target", "release", "app"))
	// 前端产物要 git 确认，另有专门的测试；这里不是仓库，同样的目录不能列出
	write(t, filepath.Join(root, "web", "package.json"))
	write(t, filepath.Join(root, "web", "dist", "index.js"))

	got := map[string]string{}
	for _, f := range walk(root) {
		rel, _ := filepath.Rel(root, f.path)
		got[filepath.ToSlash(rel)] = f.rule.kind
	}
	want := map[string]string{"gradle-app/build": "Gradle", "rs/target": "Rust"}
	assertKinds(t, got, want)
}

func TestWebOutputNeedsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v：%v %s", args, err, out)
		}
	}
	repo := filepath.Join(root, "mono")
	write(t, filepath.Join(repo, "README.md"))
	git(repo, "init", "-q")
	// 上级仓库的规则也算；更近的 !build 取消忽略
	writeText(t, filepath.Join(repo, ".gitignore"), "build\n/dist/\n")
	write(t, filepath.Join(repo, "package.json"))
	write(t, filepath.Join(repo, "dist", "index.js"))
	write(t, filepath.Join(repo, "a", "package.json"))
	write(t, filepath.Join(repo, "a", "build", "index.html"))
	write(t, filepath.Join(repo, "b", "package.json"))
	writeText(t, filepath.Join(repo, "b", ".gitignore"), "!build\n")
	write(t, filepath.Join(repo, "b", "build", "index.html"))
	// 目录虽被忽略，但里面有强制加入版本库的脚本，是源码不是产物
	write(t, filepath.Join(repo, "c", "package.json"))
	write(t, filepath.Join(repo, "c", "build", "preview.js"))
	git(repo, "add", "-f", "c/build/preview.js")
	// /dist/ 只锚定在仓库根，子项目里提交进来的 dist（库发布产物）不算
	write(t, filepath.Join(repo, "lib", "package.json"))
	write(t, filepath.Join(repo, "lib", "dist", "index.js"))
	// 被忽略但没有网页资源的 build
	write(t, filepath.Join(repo, "d", "package.json"))
	write(t, filepath.Join(repo, "d", "build", "notes.txt"))

	got := map[string]string{}
	for _, f := range walk(root) {
		rel, _ := filepath.Rel(root, f.path)
		got[filepath.ToSlash(rel)] = f.rule.kind
	}
	assertKinds(t, got, map[string]string{"mono/dist": "Node", "mono/a/build": "Node"})
}

func assertKinds(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("扫描结果不对：%v，期望 %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 应识别为 %s 产物，实际 %v", k, v, got)
		}
	}
}

func TestWalkSkipsGoModCache(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go", "pkg", "mod", "example.com", "lib@v1.0.0", "build.gradle"))
	write(t, filepath.Join(root, "go", "pkg", "mod", "example.com", "lib@v1.0.0", "build", "classes", "x"))
	write(t, filepath.Join(root, "java", "app", "build.gradle"))
	write(t, filepath.Join(root, "java", "app", "build", "libs", "app.jar"))
	// Windows 下的媒体库和 Scoop 安装目录也不进入
	write(t, filepath.Join(root, "scoop", "apps", "tool", "current", "build.gradle"))
	write(t, filepath.Join(root, "scoop", "apps", "tool", "current", "build", "libs", "x.jar"))
	fs := walk(root)
	if len(fs) != 1 || fs[0].path != filepath.Join(root, "java", "app", "build") {
		t.Fatalf("应只找到项目里的 build，不进入 Go 模块缓存：%v", fs)
	}
}

func writeText(t *testing.T, p, text string) {
	t.Helper()
	write(t, p)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
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
