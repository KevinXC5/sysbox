package jetbrains

import (
	"os"
	"path/filepath"
	"testing"
)

// fixture 在临时目录里搭一套模拟的缓存与日志结构
func fixture(t *testing.T) Options {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir())
	o := Options{
		Home:      home,
		CacheRoot: filepath.Join(home, "Library", "Caches", "JetBrains"),
		LogRoot:   filepath.Join(home, "Library", "Logs", "JetBrains"),
	}
	ide := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2")
	for _, d := range []string{"index", "caches", "full-line", "LocalHistory", "llm-jcp-outbox", "mystery"} {
		mustMkdir(t, filepath.Join(ide, d))
	}
	// acp-agents：每个 agent 只保留最新版本
	acp := filepath.Join(ide, "acp-agents")
	for _, d := range []string{
		"claude-acp/0.9.0", "claude-acp/0.10.0", "claude-acp/0.10.0-beta.1",
		"opencode/1.18.32", ".downloads/opencode/1.18.31", ".downloads/opencode/1.18.32", ".runtimes/node/24.13.0", ".runtimes/node/24.19.0",
	} {
		mustMkdir(t, filepath.Join(acp, d))
	}
	mustWrite(t, filepath.Join(acp, "registry.json"))
	mustWrite(t, filepath.Join(acp, "claude-acp", ".DS_Store"))
	mustMkdir(t, filepath.Join(o.LogRoot, "IntelliJIdea2026.2"))
	mustWrite(t, filepath.Join(ide, "icon-cache-v2.db"))
	mustWrite(t, filepath.Join(ide, ".pid"))
	return o
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClassify(t *testing.T) {
	o := fixture(t)
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, it := range items {
		got[it.Group+"/"+it.Name] = it.Category
	}
	want := map[string]int{
		"IntelliJIdea2026.2/acp-agents/claude-acp/0.10.0":           CatSkip,
		"IntelliJIdea2026.2/acp-agents/claude-acp/0.10.0-beta.1":    CatClean,
		"IntelliJIdea2026.2/acp-agents/claude-acp/0.9.0":            CatClean,
		"IntelliJIdea2026.2/acp-agents/claude-acp/.DS_Store":        CatLeftover,
		"IntelliJIdea2026.2/acp-agents/opencode/1.18.32":            CatSkip,
		"IntelliJIdea2026.2/acp-agents/.downloads/opencode/1.18.31": CatClean,
		"IntelliJIdea2026.2/acp-agents/.downloads/opencode/1.18.32": CatSkip,
		"IntelliJIdea2026.2/acp-agents/.runtimes/node/24.13.0":      CatClean,
		"IntelliJIdea2026.2/acp-agents/.runtimes/node/24.19.0":      CatSkip,
		"IntelliJIdea2026.2/acp-agents/registry.json":               CatSkip,
		"IntelliJIdea2026.2/index":                                  CatClean,
		"IntelliJIdea2026.2/caches":                                 CatClean,
		"IntelliJIdea2026.2/icon-cache-v2.db":                       CatClean,
		"IntelliJIdea2026.2/full-line":                              CatSkip,
		"IntelliJIdea2026.2/LocalHistory":                           CatKeep,
		"IntelliJIdea2026.2/.pid":                                   CatKeep,
		"IntelliJIdea2026.2/llm-jcp-outbox":                         CatLeftover,
		"IntelliJIdea2026.2/mystery":                                CatLeftover,
		"Logs/IntelliJIdea2026.2":                                   CatClean,
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("%s: 期望分类 %d，实际 %d", k, c, got[k])
		}
	}
	if len(got) != len(want) {
		t.Errorf("条目数量不符：期望 %d，实际 %d", len(want), len(got))
	}
}

// 所有分类都能手动勾选，只有可清理默认勾选；保留与未覆盖勾选后按不可恢复警示
func TestClassifySelection(t *testing.T) {
	items, err := Classify(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if !it.Selectable {
			t.Errorf("%s 应可手动勾选", it.Name)
		}
		if it.Selected != (it.Category == CatClean) {
			t.Errorf("%s 分类 %d，默认勾选应为 %v", it.Name, it.Category, it.Category == CatClean)
		}
		irreversible := it.Category == CatKeep || it.Category == CatLeftover
		if it.Irreversible != irreversible {
			t.Errorf("%s 分类 %d，不可恢复标记应为 %v", it.Name, it.Category, irreversible)
		}
	}
}

// 手动勾选的跳过项（如已卸载 Agent 的本地最新版本）可以通过删除校验
func TestRemoveSkippedAgent(t *testing.T) {
	o := fixture(t)
	target := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2", "acp-agents", "opencode", "1.18.32")
	if err := o.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("opencode 本地最新版本应该已被删除")
	}
}

// 条目本身是符号链接时只删链接，不影响目标
func TestRemoveSymlinkKeepsTarget(t *testing.T) {
	o := fixture(t)
	outside := filepath.Join(o.Home, "precious")
	mustMkdir(t, outside)
	link := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2", "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := o.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("符号链接应该已被删除")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("链接目标不应被删除")
	}
}

func TestClassifyMissingRoot(t *testing.T) {
	o := Options{CacheRoot: filepath.Join(t.TempDir(), "none")}
	if _, err := Classify(o); err != ErrNoCache {
		t.Fatalf("期望 ErrNoCache，实际 %v", err)
	}
}

func TestRemove(t *testing.T) {
	o := fixture(t)
	target := filepath.Join(o.CacheRoot, "IntelliJIdea2026.2", "index")
	if err := o.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("index 应该已被删除")
	}
}

func TestRemoveRejectsOutside(t *testing.T) {
	o := fixture(t)
	for _, p := range []string{o.CacheRoot, o.Home, "/", filepath.Join(o.CacheRoot, "..", "evil")} {
		if err := o.Remove(p); err == nil {
			t.Errorf("越界路径应被拒绝：%s", p)
		}
	}
}

// 父目录是指向根外的符号链接时，解析后越界必须拒绝
func TestRemoveRejectsSymlinkEscape(t *testing.T) {
	o := fixture(t)
	outside := filepath.Join(o.Home, "precious")
	mustMkdir(t, filepath.Join(outside, "data"))
	link := filepath.Join(o.CacheRoot, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := o.Remove(filepath.Join(link, "data")); err == nil {
		t.Fatal("通过符号链接逃逸的路径应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(outside, "data")); err != nil {
		t.Fatal("根外的数据不应被删除")
	}
}
