package projects

import (
	"os"
	"path/filepath"
	"strings"
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

var rules = []rule{
	{dir: "node_modules", kind: "Node", markers: nodeMarkers},
	{dir: ".next", kind: "Next.js", markers: nodeMarkers},
	{dir: ".nuxt", kind: "Nuxt", markers: nodeMarkers},
	{dir: ".svelte-kit", kind: "SvelteKit", markers: nodeMarkers},
	{dir: ".turbo", kind: "Turborepo", markers: nodeMarkers},
	{dir: ".parcel-cache", kind: "Parcel", markers: nodeMarkers},
	{dir: "target", kind: "Rust", markers: []string{"Cargo.toml"}},
	{dir: "target", kind: "Maven", markers: []string{"pom.xml"}},
	{dir: "build", kind: "Gradle", markers: gradleMarkers},
	{dir: ".gradle", kind: "Gradle", markers: gradleMarkers},
	{dir: "build", kind: "Flutter", markers: []string{"pubspec.yaml"}},
	{dir: ".dart_tool", kind: "Dart", markers: []string{"pubspec.yaml"}},
	{dir: ".build", kind: "Swift", markers: []string{"Package.swift"}},
	{dir: "obj", kind: ".NET", markers: dotnetMarkers},
	{dir: "bin", kind: ".NET", markers: dotnetMarkers},
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

// skipDirs 扫描时不进入的目录：系统目录、版本库和依赖目录本身
var skipDirs = map[string]bool{
	"Library": true, "AppData": true, "Applications": true, "node_modules": true,
	".git": true, ".hg": true, ".svn": true, ".Trash": true,
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

// isVenv 虚拟环境根目录必有 pyvenv.cfg
func isVenv(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, "pyvenv.cfg"))
	return err == nil && fi.Mode().IsRegular()
}
