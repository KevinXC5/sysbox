//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceExecutableMovesOldAside(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sysbox.exe")
	tmp := filepath.Join(dir, ".sysbox-update-test")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(tmp, exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new" {
		t.Fatalf("新文件未就位：%q %v", got, err)
	}
	old, err := os.ReadFile(exe + oldSuffix)
	if err != nil || string(old) != "old" {
		t.Fatalf("旧文件应改名为 .old：%q %v", old, err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("临时文件应已被移走")
	}
}

func TestReplaceExecutableRestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sysbox.exe")
	// 不存在的临时文件会让第二步 rename 失败，旧文件应被改回原路径
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(filepath.Join(dir, "missing"), exe); err == nil {
		t.Fatal("临时文件不存在时应失败")
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old" {
		t.Fatalf("失败后应恢复旧版本：%q %v", got, err)
	}
	if _, err := os.Stat(exe + oldSuffix); !os.IsNotExist(err) {
		t.Fatal("恢复后不应留下 .old")
	}
}

func TestCleanupOldRemovesPreviousBinary(t *testing.T) {
	// CleanupOld 按 os.Executable 定位，测试里用当前测试二进制模拟一次残留清理
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := exe + oldSuffix
	if err := os.WriteFile(old, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(old) })
	CleanupOld()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("CleanupOld 应删除 %s：%v", old, err)
	}
}
