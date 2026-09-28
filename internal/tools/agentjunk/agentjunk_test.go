package agentjunk

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

var now = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestCompareVersion(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.10.0"}
	for i := 1; i < len(ordered); i++ {
		a, _ := parseVersion(ordered[i-1], semverName)
		b, _ := parseVersion(ordered[i], semverName)
		if compareVersion(a, b) >= 0 {
			t.Errorf("%s 应小于 %s", ordered[i-1], ordered[i])
		}
	}
	// 平台后缀与构建元数据不影响比较
	a, _ := parseVersion("0.157.1-aarch64-apple-darwin", semverName)
	b, _ := parseVersion("0.157.1+build.5", semverName)
	if compareVersion(a, b) != 0 || !a.release {
		t.Errorf("平台后缀应被忽略：%+v %+v", a, b)
	}
	if _, ok := parseVersion("grok-1.0.41-macos-aarch64", grokName); !ok {
		t.Error("grok 版本名应能解析")
	}
	for _, name := range []string{"grok-1.0.41-linux-x86_64", "grok-1.2.0-windows-x64.exe", "grok-1.2.0-windows-arm64"} {
		v, ok := parseVersion(name, grokName)
		if !ok || !v.release {
			t.Errorf("grok 版本名应解析为正式版：%s %+v", name, v)
		}
	}
}

// setAge 把路径及其下所有条目的修改时间设为 days 天前
func setAge(t *testing.T, p string, days int) {
	t.Helper()
	ts := now.Add(-time.Duration(days) * 24 * time.Hour)
	_ = filepath.WalkDir(p, func(q string, _ fs.DirEntry, _ error) error {
		return os.Chtimes(q, ts, ts)
	})
}

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("data"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) *Source {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir())
	cache := filepath.Join(home, ".claude/cache")
	write(t, filepath.Join(cache, "old/a.bin"))
	write(t, filepath.Join(cache, "fresh/b.bin"))
	write(t, filepath.Join(home, ".grok/logs/run.log"))
	mustLink(t, "/etc", filepath.Join(cache, "linked/escape"))

	// Claude 版本：当前 1.1.0，最高 1.2.0，只有 1.0.0 可删
	versions := filepath.Join(home, ".local/share/claude/versions")
	for _, v := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		write(t, filepath.Join(versions, v))
	}
	mustLink(t, filepath.Join(versions, "1.1.0"), filepath.Join(home, ".local/bin/claude"))

	setAge(t, filepath.Join(cache, "old"), 60)
	setAge(t, filepath.Join(cache, "linked"), 60)
	setAge(t, filepath.Join(cache, "fresh"), 3)
	setAge(t, filepath.Join(home, ".grok/logs"), 90)
	setAge(t, versions, 10)
	return &Source{Home: home, KeepDays: 30, Now: func() time.Time { return now }}
}

func mustLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		// Windows CI 默认没有创建符号链接的权限，reparse buffer 无效时跳过而不是失败
		if runtime.GOOS == "windows" {
			t.Skipf("无法创建符号链接：%v", err)
		}
		t.Fatal(err)
	}
}

func scan(t *testing.T, s *Source) map[string]cleanup.Item {
	t.Helper()
	items, err := s.Scan(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]cleanup.Item{}
	for _, it := range items {
		rel, _ := filepath.Rel(s.Home, it.Path)
		// 查找键统一用 "/"，避免 Windows 上 Rel 返回反斜杠对不上测试里的期望路径
		m[filepath.ToSlash(rel)] = it
	}
	return m
}

func TestScan(t *testing.T) {
	s := fixture(t)
	got := scan(t, s)
	want := map[string]int{
		".claude/cache/old":                  CatCache,
		".claude/cache/fresh":                CatRecent,
		".claude/cache/linked":               CatReview,
		".grok/logs/run.log":                 CatLog,
		".local/share/claude/versions/1.0.0": CatOld,
	}
	for rel, cat := range want {
		it, ok := got[rel]
		if !ok || it.Category != cat {
			t.Errorf("%s：期望分类 %d，实际 %+v", rel, cat, it)
		}
	}
	for _, keep := range []string{".local/share/claude/versions/1.1.0", ".local/share/claude/versions/1.2.0"} {
		if _, ok := got[keep]; ok {
			t.Errorf("当前版本和最高版本不应出现：%s", keep)
		}
	}
	if got[".grok/logs/run.log"].Selected {
		t.Error("日志默认不应勾选")
	}
	if !got[".claude/cache/old"].Selected {
		t.Error("过期缓存默认应勾选")
	}
}

func TestRemoveRechecks(t *testing.T) {
	s := fixture(t)
	items := scan(t, s)
	old := items[".claude/cache/old"]

	// 扫描后目录被替换成同名新目录，inode 变化必须拒绝删除。
	// 先建好替身再换名：ext4 会立刻复用刚删目录的 inode，删完再重建会得到同一个号。
	replacement := old.Path + ".replacement"
	write(t, filepath.Join(replacement, "new.bin"))
	setAge(t, replacement, 60)
	if err := os.RemoveAll(old.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, old.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(old); err == nil {
		t.Fatal("目标被替换后应拒绝删除")
	}

	// 正常条目可以删除
	v := items[".local/share/claude/versions/1.0.0"]
	if err := s.Remove(v); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.Path); !os.IsNotExist(err) {
		t.Fatal("旧版本应已删除")
	}
}

func TestRemoveRejectsActiveSwitch(t *testing.T) {
	s := fixture(t)
	v := scan(t, s)[".local/share/claude/versions/1.0.0"]
	// 扫描后当前版本切换到了待删条目
	link := filepath.Join(s.Home, ".local/bin/claude")
	_ = os.Remove(link)
	mustLink(t, v.Path, link)
	if err := s.Remove(v); err == nil {
		t.Fatal("当前版本切换到该条目后应拒绝删除")
	}
}

func TestWindowsCopiesAreReviewOnly(t *testing.T) {
	prev := goos
	goos = "windows"
	t.Cleanup(func() { goos = prev })

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	versions := filepath.Join(home, ".local/share/claude/versions")
	for _, v := range []string{"1.0.0.exe", "1.1.0.exe", "1.2.0.exe"} {
		write(t, filepath.Join(versions, v))
	}
	// 入口是复制品，不是指向版本目录的链接
	write(t, filepath.Join(home, ".local/bin/claude.exe"))

	releases := filepath.Join(home, ".codex/packages/standalone/releases")
	write(t, filepath.Join(releases, "0.1.0", "bin/codex.exe"))
	write(t, filepath.Join(releases, "0.2.0", "bin/codex.exe"))
	write(t, filepath.Join(home, ".codex/packages/standalone/current"))

	downloads := filepath.Join(home, ".grok/downloads")
	write(t, filepath.Join(downloads, "grok-1.0.41-windows-x64.exe"))
	write(t, filepath.Join(downloads, "grok-1.0.42-windows-x64.exe"))
	write(t, filepath.Join(home, ".grok/bin/grok.exe"))
	write(t, filepath.Join(home, ".grok/bin/agent.exe"))

	write(t, filepath.Join(home, ".opencode/bin/opencode.exe"))
	write(t, filepath.Join(home, ".local/bin/opencode.exe"))
	write(t, filepath.Join(home, ".opencode/bin/opencode-1.2.3.exe"))

	s := &Source{Home: home, KeepDays: 30, Now: func() time.Time { return now }}
	got := scan(t, s)

	for _, rel := range []string{
		".local/share/claude/versions/1.0.0.exe",
		".local/share/claude/versions/1.1.0.exe",
		".codex/packages/standalone/releases/0.1.0",
		".grok/downloads/grok-1.0.41-windows-x64.exe",
	} {
		it, ok := got[rel]
		if !ok || it.Category != CatReview || it.Selectable {
			t.Errorf("%s 应只展示不删除，实际 %+v", rel, it)
		}
	}
	bak := got[".opencode/bin/opencode-1.2.3.exe"]
	if bak.Category != CatReview {
		t.Errorf("Windows 备份应进入待核查：%+v", bak)
	}
}
