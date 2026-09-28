package jetbrains

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// layoutFixture 搭一套 Windows 布局：日志在产品缓存的 log 子目录，配置在旁边。
func layoutFixture(t *testing.T, o Options) Options {
	t.Helper()
	ide := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2")
	for _, d := range []string{"index", "caches", "full-line", "LocalHistory", "log", "mystery"} {
		mustMkdir(t, filepath.Join(ide, d))
	}
	mustWrite(t, filepath.Join(ide, "log", "idea.log"))
	mustWrite(t, filepath.Join(ide, "icon-cache-v2.db"))
	mustWrite(t, filepath.Join(ide, ".pid"))
	// acp-agents 仍在产品目录内，规则与 macOS 相同
	acp := filepath.Join(ide, "acp-agents")
	mustMkdir(t, filepath.Join(acp, "claude-acp", "0.9.0"))
	mustMkdir(t, filepath.Join(acp, "claude-acp", "0.10.0"))
	// 配置里放点东西，确认扫描不会把它们算进来
	if o.AppSupport != "" {
		cfg := filepath.Join(o.AppSupport, "IntelliJIdea2026.2", "options")
		mustMkdir(t, cfg)
		mustWrite(t, filepath.Join(cfg, "other.xml"))
	}
	return o
}

func TestClassifyLogInsideCache(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := layoutFixture(t, Options{
		Home:           home,
		CacheRoot:      filepath.Join(home, "cache", "JetBrains"),
		LogInsideCache: true,
		AppSupport:     filepath.Join(home, "config", "JetBrains"),
		CacheLabel:     "cache/JetBrains",
		ConfigLabel:    "config/JetBrains",
	})

	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, it := range items {
		got[it.Group+"/"+it.Name] = it.Category
		if within(it.Path, o.AppSupport) || it.Path == o.AppSupport {
			t.Errorf("不应扫描配置：%s", it.Path)
		}
	}
	want := map[string]int{
		"IntelliJIdea2026.2/index":                        CatClean,
		"IntelliJIdea2026.2/caches":                       CatClean,
		"IntelliJIdea2026.2/icon-cache-v2.db":             CatClean,
		"IntelliJIdea2026.2/full-line":                    CatSkip,
		"IntelliJIdea2026.2/LocalHistory":                 CatKeep,
		"IntelliJIdea2026.2/.pid":                         CatKeep,
		"IntelliJIdea2026.2/mystery":                      CatLeftover,
		"Logs/IntelliJIdea2026.2/log":                     CatClean,
		"IntelliJIdea2026.2/acp-agents/claude-acp/0.10.0": CatSkip,
		"IntelliJIdea2026.2/acp-agents/claude-acp/0.9.0":  CatClean,
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("%s: 期望分类 %d，实际 %d", k, c, got[k])
		}
	}
	if len(got) != len(want) {
		t.Errorf("条目数量不符：期望 %d，实际 %d\n%v", len(want), len(got), got)
	}

	// 日志只出现一次，且路径在缓存目录内部
	var logs int
	for _, it := range items {
		if it.Name == "log" {
			logs++
			if !within(it.Path, o.CacheRoot) {
				t.Errorf("日志应位于缓存根内：%s", it.Path)
			}
		}
	}
	if logs != 1 {
		t.Fatalf("日志应只统计一次，实际 %d", logs)
	}
}

// 日志根留空时不能退化成扫描当前目录
func TestClassifyEmptyLogRoot(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := layoutFixture(t, Options{
		Home:      home,
		CacheRoot: filepath.Join(home, "cache", "JetBrains"),
		LogRoot:   "",
	})
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if !within(it.Path, home) {
			t.Fatalf("空日志根扫到了临时目录之外：%s", it.Path)
		}
	}
}

func TestRemoveInsideCacheLog(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := layoutFixture(t, Options{
		Home:           home,
		CacheRoot:      filepath.Join(home, "cache", "JetBrains"),
		LogInsideCache: true,
		AppSupport:     filepath.Join(home, "config", "JetBrains"),
	})
	logDir := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2", "log")
	if err := o.Remove(logDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Fatal("log 应该已被删除")
	}
	// 配置和缓存根本身不能删
	for _, p := range []string{
		o.AppSupport,
		filepath.Join(o.AppSupport, "IntelliJIdea2026.2"),
		o.CacheRoot,
		o.Home,
	} {
		if err := o.Remove(p); err == nil {
			t.Errorf("越界路径应被拒绝：%s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(o.AppSupport, "IntelliJIdea2026.2", "options", "other.xml")); err != nil {
		t.Fatal("配置文件不应被删除")
	}
}

func TestRootsSkipEmbeddedLog(t *testing.T) {
	home := t.TempDir()
	s := &Source{Opts: Options{
		Home:           home,
		CacheRoot:      filepath.Join(home, "cache", "JetBrains"),
		LogRoot:        filepath.Join(home, "cache", "JetBrains", "IntelliJIdea2026.2", "log"),
		LogInsideCache: true,
		AppSupport:     filepath.Join(home, "config", "JetBrains"),
		CacheLabel:     "cache/JetBrains",
		ConfigLabel:    "config/JetBrains",
	}}
	roots := s.Roots()
	if len(roots) != 2 {
		t.Fatalf("日志在缓存内时不应单独列出日志根，实际 %d 项", len(roots))
	}
	if roots[0].Path != s.Opts.CacheRoot || roots[1].Path != s.Opts.AppSupport || !roots[1].Untouched {
		t.Fatalf("根目录不符合预期：%+v", roots)
	}
}

func TestPlatformOptions(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	lookup := func(k string) string { return env[k] }
	o := platformOptions(home, lookup)

	switch runtime.GOOS {
	case "darwin":
		if o.LogInsideCache {
			t.Fatal("macOS 日志应是独立目录")
		}
		if o.CacheRoot != filepath.Join(home, "Library", "Caches", "JetBrains") {
			t.Fatalf("缓存路径不对：%s", o.CacheRoot)
		}
		if o.LogRoot != filepath.Join(home, "Library", "Logs", "JetBrains") {
			t.Fatalf("日志路径不对：%s", o.LogRoot)
		}
		if o.AppSupport != filepath.Join(home, "Library", "Application Support", "JetBrains") {
			t.Fatalf("配置路径不对：%s", o.AppSupport)
		}
	case "windows":
		if !o.LogInsideCache || o.LogRoot != "" {
			t.Fatalf("Windows 日志应留在缓存内部，LogRoot=%q", o.LogRoot)
		}
		if o.CacheRoot != filepath.Join(home, "AppData", "Local", "JetBrains") {
			t.Fatalf("默认缓存路径不对：%s", o.CacheRoot)
		}
		if o.AppSupport != filepath.Join(home, "AppData", "Roaming", "JetBrains") {
			t.Fatalf("默认配置路径不对：%s", o.AppSupport)
		}
		env["LOCALAPPDATA"] = filepath.Join(home, "CustomLocal")
		env["APPDATA"] = filepath.Join(home, "CustomRoaming")
		o = platformOptions(home, lookup)
		if o.CacheRoot != filepath.Join(home, "CustomLocal", "JetBrains") {
			t.Fatalf("LOCALAPPDATA 未生效：%s", o.CacheRoot)
		}
		if o.AppSupport != filepath.Join(home, "CustomRoaming", "JetBrains") {
			t.Fatalf("APPDATA 未生效：%s", o.AppSupport)
		}
	default:
		t.Skip("未覆盖的平台")
	}
}
