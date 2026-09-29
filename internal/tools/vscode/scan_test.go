package vscode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func TestParseVersion(t *testing.T) {
	newer, okN := parseVersion("1.85.2")
	older, okO := parseVersion("1.85.2-insider")
	if !okN || !okO || compareVersion(older, newer) >= 0 {
		t.Fatal("正式版应高于同号预发布版")
	}
	if _, ok := parseVersion("abc123deadbeef"); ok {
		t.Fatal("哈希不能当版本")
	}
	if !olderThan("1.84.0", "1.85.0") || olderThan("1.85.0", "1.85.0") {
		t.Fatal("版本比较不符合预期")
	}
}

func TestClassifyKeepsUnknownAndReferenced(t *testing.T) {
	o := fixture(t)
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]itemView{}
	for _, it := range items {
		got[it.Group+"/"+it.Name] = itemView{it.Category, it.Selected}
	}
	want := map[string]itemView{
		"Code/Cache":                       {CatClean, true},
		"Code/CachedData":                  {CatClean, true},
		"Code/Code Cache":                  {CatClean, true},
		"Code/GPUCache":                    {CatClean, true},
		"Code/DawnGraphiteCache":           {CatClean, true},
		"Code/CachedExtensionVSIXs":        {CatClean, true},
		"Code/logs":                        {CatClean, true},
		"Code/Crashpad":                    {CatClean, true},
		"Code/User":                        {CatKeep, false},
		"Code/Backups":                     {CatKeep, false},
		"Code/Session Storage":             {CatKeep, false},
		"Code/mystery":                     {CatKeep, false},
		"Code/Local State":                 {CatKeep, false},
		"Code/extensions/pub.sample-1.0.0": {CatClean, true},
		"Code/extensions/pub.sample-1.2.0": {CatKeep, false},
		"Code/extensions/pub.sample-1.1.0": {CatKeep, false},
		"Code/extensions/pub.sample-1.3.0-darwin-arm64": {CatKeep, false},
		"Code/extensions/pub.sample-1.0.0-darwin-arm64": {CatClean, true},
		"Code/extensions/pub.sample-1.0.0-win32-x64":    {CatKeep, false},
		"Code/cli/aaaaaaaa":                             {CatSkip, false},
		"Code/cli/bbbbbbbb":                             {CatKeep, false},
		"Code/cli/cccccccc":                             {CatKeep, false},
		"Code/cli/aaaaaaaa/logs":                        {CatClean, true},
		"Code/cli/nodata":                               {CatKeep, false},
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: 期望 %+v，实际 %+v", k, w, got[k])
		}
	}
	for k, v := range got {
		if strings.Contains(k, "User/settings") || strings.Contains(k, "workspaceStorage") {
			t.Errorf("不应展开用户数据：%s %+v", k, v)
		}
	}
}

func TestClassifyBrokenIndexSkipsExtensions(t *testing.T) {
	o := fixture(t)
	if err := os.WriteFile(filepath.Join(o.Channels[0].ExtRoot, "extensions.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if strings.Contains(it.Path, "extensions") && it.Category == CatClean && strings.Contains(it.Name, "sample") {
			t.Fatalf("索引损坏时不应清理扩展：%s", it.Path)
		}
	}
	found := false
	for _, it := range items {
		if it.Name == "extensions" && it.Category == CatSkip {
			found = true
		}
	}
	if !found {
		t.Fatal("索引损坏应整组跳过扩展")
	}
}

func TestProfileReferenceProtectsOld(t *testing.T) {
	o := fixture(t)
	profile := filepath.Join(o.Channels[0].AppRoot, "User", "profiles", "work")
	mustMkdir(t, profile)
	writeIndex(t, filepath.Join(profile, "extensions.json"), []idxRow{{"pub.sample", "1.0.0", "pub.sample-1.0.0"}})
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Name == "pub.sample-1.0.0" && it.Category != CatKeep {
			t.Fatalf("配置档引用的旧版本必须保留，实际 %d", it.Category)
		}
	}
}

func TestRemoveRejectsEscapeAndRoots(t *testing.T) {
	o := fixture(t)
	cache := filepath.Join(o.Channels[0].AppRoot, "Cache")
	if err := o.Remove(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("Cache 应该已被删除")
	}
	outside := filepath.Join(o.Home, "precious")
	mustMkdir(t, filepath.Join(outside, "data"))
	link := filepath.Join(o.Channels[0].AppRoot, "escape")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("当前环境无法创建符号链接：%v", err)
		}
		t.Fatal(err)
	}
	if err := o.Remove(filepath.Join(link, "data")); err == nil {
		t.Fatal("符号链接逃逸应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(outside, "data")); err != nil {
		t.Fatal("根外数据不应被删除")
	}
	user := filepath.Join(o.Channels[0].AppRoot, "User")
	for _, p := range []string{o.Home, o.Channels[0].AppRoot, user, filepath.Join(user, "settings.json"), "/"} {
		if err := o.Remove(p); err == nil {
			t.Errorf("越界路径应被拒绝：%s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(user, "settings.json")); err != nil {
		t.Fatal("设置不应被删除")
	}
}

func TestRemoveOptionalCLIOnly(t *testing.T) {
	o := fixture(t)
	old := filepath.Join(o.Channels[0].CLIRoots[0], "aaaaaaaa")
	if err := o.Remove(old); err == nil {
		t.Fatal("旧服务端不能走默认可清理删除")
	}
	if err := o.RemoveOptional(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("手动确认后旧服务端应删除")
	}
	latest := filepath.Join(o.Channels[0].CLIRoots[0], "bbbbbbbb")
	if err := o.RemoveOptional(latest); err == nil {
		t.Fatal("最新服务端不能删除")
	}
}

func TestRunningBlocks(t *testing.T) {
	s := &Source{Opts: Options{Channels: []Channel{{
		ProcNames: []string{"Code", "Code Helper"},
	}}}, Runner: fakeRunner{out: "42 0.0 0:01.00 0:10 100 S /Applications/Visual Studio Code.app/Contents/MacOS/Code\n"}}
	if runtime.GOOS == "windows" {
		s.Runner = fakeRunner{out: ""}
		// Windows 快照不走 ps；这里只验证名字过滤函数
		if !sysx.ByName("Code")(sysx.Proc{Path: "Code.exe"}) {
			t.Fatal("Windows 上 Code.exe 应匹配 Code")
		}
		return
	}
	n := s.Check()
	if n == nil || !n.Blocking {
		t.Fatal("Code 运行时应阻止清理")
	}
}

func TestPlatformLayout(t *testing.T) {
	home := t.TempDir()
	o := platformOptions(home, func(k string) string {
		if k == "APPDATA" {
			return filepath.Join(home, "Roaming")
		}
		if k == "USERPROFILE" {
			return home
		}
		return ""
	})
	if len(o.Channels) != 2 {
		t.Fatalf("应同时覆盖稳定版与 Insiders，实际 %d", len(o.Channels))
	}
	stable := o.Channels[0]
	if runtime.GOOS == "windows" {
		if stable.AppRoot != filepath.Join(home, "Roaming", "Code") {
			t.Fatalf("Windows 应用路径不对：%s", stable.AppRoot)
		}
	} else if runtime.GOOS == "darwin" {
		if !strings.HasSuffix(stable.AppRoot, filepath.Join("Application Support", "Code")) {
			t.Fatalf("macOS 应用路径不对：%s", stable.AppRoot)
		}
		if !strings.Contains(stable.ExtRoot, ".vscode") {
			t.Fatalf("扩展路径不对：%s", stable.ExtRoot)
		}
	}
}

type itemView struct {
	cat      int
	selected bool
}

type idxRow struct {
	id, version, rel string
}

func fixture(t *testing.T) Options {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(home, "Code")
	ext := filepath.Join(home, "extensions")
	cli := filepath.Join(home, "cli", "servers")
	for _, d := range []string{
		"Cache", "CachedData", "Code Cache", "GPUCache", "DawnGraphiteCache",
		"CachedExtensionVSIXs", "logs", "Crashpad", "User", "Backups", "Session Storage", "mystery",
	} {
		mustMkdir(t, filepath.Join(app, d))
	}
	mustWrite(t, filepath.Join(app, "User", "settings.json"), []byte(`{"editor.fontSize":14}`))
	mustWrite(t, filepath.Join(app, "Local State"), []byte("x"))
	writeIndex(t, filepath.Join(ext, "extensions.json"), []idxRow{
		{"pub.sample", "1.2.0", "pub.sample-1.2.0"},
		{"pub.sample", "1.1.0", "pub.sample-1.1.0"},
		{"pub.sample", "1.3.0", "pub.sample-1.3.0-darwin-arm64"},
		{"pub.sample", "1.0.0", "pub.sample-1.0.0-win32-x64"},
	})
	writeExt(t, ext, "pub", "sample", "1.0.0", "")
	writeExt(t, ext, "pub", "sample", "1.2.0", "")
	writeExt(t, ext, "pub", "sample", "1.1.0", "")
	writeExt(t, ext, "pub", "sample", "1.3.0", "darwin-arm64")
	writeExt(t, ext, "pub", "sample", "1.0.0", "darwin-arm64")
	writeExt(t, ext, "pub", "sample", "1.0.0", "win32-x64")
	mustWrite(t, filepath.Join(ext, ".obsolete"), []byte(`{"pub.sample-1.0.0":true}`))
	for _, name := range []string{"aaaaaaaa", "bbbbbbbb", "cccccccc"} {
		mustMkdir(t, filepath.Join(cli, name))
	}
	mustWrite(t, filepath.Join(cli, "aaaaaaaa", "product.json"), []byte(`{"version":"1.80.0","quality":"stable"}`))
	mustMkdir(t, filepath.Join(cli, "aaaaaaaa", "logs"))
	mustWrite(t, filepath.Join(cli, "bbbbbbbb", "product.json"), []byte(`{"version":"1.85.0","quality":"stable"}`))
	mustWrite(t, filepath.Join(cli, "cccccccc", "product.json"), []byte(`{"version":"1.85.0-insider","quality":"insider"}`))
	mustMkdir(t, filepath.Join(cli, "nodata"))
	return Options{Home: home, Channels: []Channel{{
		Name: "Code", AppRoot: app, ExtRoot: ext, CLIRoots: []string{cli},
		ProcNames: []string{"Code"},
	}}}
}

func writeExt(t *testing.T, root, pub, name, ver, platform string) {
	t.Helper()
	dir := pub + "." + name + "-" + ver
	if platform != "" {
		dir += "-" + platform
	}
	p := filepath.Join(root, dir)
	mustMkdir(t, p)
	body, _ := json.Marshal(map[string]string{"publisher": pub, "name": name, "version": ver})
	mustWrite(t, filepath.Join(p, "package.json"), body)
}

func writeIndex(t *testing.T, path string, rows []idxRow) {
	t.Helper()
	var entries []map[string]any
	for _, r := range rows {
		entries = append(entries, map[string]any{
			"version":          r.version,
			"relativeLocation": r.rel,
			"identifier":       map[string]string{"id": r.id},
		})
	}
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, b)
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakeRunner struct {
	out string
	err error
}

func (f fakeRunner) Run(context.Context, sysx.Cmd) (string, error) { return f.out, f.err }

func TestProcessQueryFailureBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用系统进程快照，不经过 Runner")
	}
	s := &Source{Opts: Options{Channels: []Channel{{ProcNames: []string{"Code"}}}}, Runner: fakeRunner{err: errors.New("进程查询失败")}}
	if n := s.Check(); n == nil || !n.Blocking {
		t.Fatal("无法确认运行状态时必须阻止清理")
	}
}

func TestRootsIncludeServers(t *testing.T) {
	s := &Source{Opts: fixture(t)}
	roots := s.Roots()
	want := s.Opts.Channels[0].CLIRoots[0]
	for _, root := range roots {
		if root.Path == want {
			return
		}
	}
	t.Fatal("服务端目录必须计入清理前后的空间统计")
}
