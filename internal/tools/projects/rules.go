package projects

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// rule 一种构建产物：目录名匹配，且所在目录有对应的项目标记文件才算数，
// 避免把手工建的同名目录当成产物删掉。
type rule struct {
	dir     string   // 产物目录名
	kind    string   // 项目类型，界面说明用
	markers []string // 父目录中存在任一即确认项目类型，支持 *.csproj 这样的通配
	manual  bool     // 默认不勾选：重建依赖清单之外的信息，如虚拟环境里手动装的包
	check   func(dir string) bool
}

var (
	nodeMarkers   = []string{"package.json"}
	gradleMarkers = []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}
	pyMarkers     = []string{"pyproject.toml", "requirements.txt", "setup.py", "Pipfile"}
	dotnetMarkers = []string{"*.csproj", "*.fsproj", "*.vbproj"}
)

// build、target、bin 这类常见目录名也可能是手写的脚本或源码目录，
// 除了父目录的项目标记，还要求目录里有对应构建工具留下的特征文件。
var (
	rustTarget   = hasAny("CACHEDIR.TAG", ".rustc_info.json", "debug", "release")
	mavenTarget  = hasAny("classes", "test-classes", "maven-status", "maven-archiver", "generated-sources", "surefire-reports", "*.jar", "*.war")
	gradleBuild  = hasAny("tmp", "classes", "libs", "generated", "kotlin", "intermediates", "test-results")
	flutterBuild = hasAny(".last_build_id", "flutter_assets", "native_assets", "app", "ios")
	dotnetObj    = hasAny("project.assets.json", "*.nuget.g.props", "*.nuget.dgspec.json", "Debug", "Release")
	dotnetBin    = hasAny("Debug", "Release")
	// 前端打包输出没有统一的特征文件，要求有网页资源、且被 git 忽略又没有受控文件，
	// 排除提交进仓库的 dist（库发布产物）和放构建脚本的 build 目录
	webOutput = func(dir string) bool {
		return hasAny("*.js", "*.mjs", "*.cjs", "*.html", "*.css", "static", "assets")(dir) && gitIgnored(dir)
	}
)

var rules = []rule{
	{dir: "node_modules", kind: "Node", markers: nodeMarkers},
	{dir: ".next", kind: "Next.js", markers: nodeMarkers},
	{dir: ".nuxt", kind: "Nuxt", markers: nodeMarkers},
	{dir: ".svelte-kit", kind: "SvelteKit", markers: nodeMarkers},
	{dir: ".turbo", kind: "Turborepo", markers: nodeMarkers},
	{dir: ".parcel-cache", kind: "Parcel", markers: nodeMarkers},
	{dir: "build", kind: "Node", markers: nodeMarkers, check: webOutput},
	{dir: "dist", kind: "Node", markers: nodeMarkers, check: webOutput},
	{dir: "target", kind: "Rust", markers: []string{"Cargo.toml"}, check: rustTarget},
	{dir: "target", kind: "Maven", markers: []string{"pom.xml"}, check: mavenTarget},
	{dir: "build", kind: "Gradle", markers: gradleMarkers, check: gradleBuild},
	{dir: ".gradle", kind: "Gradle", markers: gradleMarkers},
	{dir: "build", kind: "Flutter", markers: []string{"pubspec.yaml"}, check: flutterBuild},
	{dir: ".dart_tool", kind: "Dart", markers: []string{"pubspec.yaml"}},
	{dir: ".build", kind: "Swift", markers: []string{"Package.swift"}},
	{dir: "obj", kind: ".NET", markers: dotnetMarkers, check: dotnetObj},
	{dir: "bin", kind: ".NET", markers: dotnetMarkers, check: dotnetBin},
	{dir: ".zig-cache", kind: "Zig", markers: []string{"build.zig"}},
	{dir: "zig-out", kind: "Zig", markers: []string{"build.zig"}},
	{dir: ".venv", kind: "Python", markers: pyMarkers, manual: true, check: isVenv},
	{dir: "venv", kind: "Python", markers: pyMarkers, manual: true, check: isVenv},
}

// artifactNames 所有产物目录名，计算项目活跃时间时跳过
var artifactNames = func() map[string]bool {
	m := map[string]bool{}
	for _, r := range rules {
		m[r.dir] = true
	}
	return m
}()

// skipDirs 扫描时不进入的目录：系统目录、媒体库、软件安装目录、版本库和依赖目录本身。
// Windows 家目录下的 Application Data 等兼容性 junction 不会被当作目录进入。
var skipDirs = map[string]bool{
	"Library": true, "AppData": true, "Applications": true, "node_modules": true,
	"Pictures": true, "Music": true, "Movies": true, "Videos": true,
	"scoop": true, "anaconda3": true, "miniconda3": true, "miniforge3": true,
	".git": true, ".hg": true, ".svn": true, ".Trash": true,
}

// skipped 不进入的目录。Go 模块缓存 pkg/mod 里是别人的源码，其中的同名目录不能当产物删
func skipped(parent, name string) bool {
	return skipDirs[name] || strings.HasPrefix(name, ".") || (name == "mod" && filepath.Base(parent) == "pkg")
}

// match 返回 parent 下名为 name 的目录对应的规则
func match(parent, name string) (rule, bool) {
	for _, r := range rules {
		if r.dir != name || !hasMarker(parent, r.markers) {
			continue
		}
		if r.check != nil && !r.check(filepath.Join(parent, name)) {
			continue
		}
		return r, true
	}
	return rule{}, false
}

func hasMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if strings.ContainsAny(m, "*?[") {
			if found, _ := filepath.Glob(filepath.Join(dir, m)); len(found) > 0 {
				return true
			}
			continue
		}
		if fi, err := os.Lstat(filepath.Join(dir, m)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// hasAny 返回检查函数：目录里存在任一名称（支持通配）即为真
func hasAny(names ...string) func(dir string) bool {
	return func(dir string) bool {
		for _, n := range names {
			if strings.ContainsAny(n, "*?[") {
				if found, _ := filepath.Glob(filepath.Join(dir, n)); len(found) > 0 {
					return true
				}
				continue
			}
			if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
				return true
			}
		}
		return false
	}
}

// gitIgnored dir 被 git 忽略，且里面没有强制加入版本库的文件。
// 交给 git 判断，规则的层级、锚定和取反都与 git 一致；
// 没装 git 或不在仓库里时无法确认，一律不算产物。
func gitIgnored(dir string) bool {
	project, name := filepath.Dir(dir), filepath.Base(dir)
	// 仓库在网络盘等情况下 git 可能很慢，超时按无法确认处理
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, "git", "-C", project, "check-ignore", "-q", "--", name+"/").Run() != nil {
		return false
	}
	out, err := exec.CommandContext(ctx, "git", "-C", project, "ls-files", "--", name).Output()
	return err == nil && len(bytes.TrimSpace(out)) == 0
}

// isVenv 虚拟环境根目录必有 pyvenv.cfg
func isVenv(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, "pyvenv.cfg"))
	return err == nil && fi.Mode().IsRegular()
}
