//go:build windows

package uninstall

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitGone 等待后台 cmd 删除文件，最多 15 秒
func waitGone(t *testing.T, path string) bool {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func TestRemoveExecutableCleansEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Programs dir", "sysbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "sysbox.exe")
	for _, f := range []string{exe, exe + ".old"} {
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeExecutable(exe); err != nil {
		t.Fatal(err)
	}
	if !waitGone(t, dir) {
		t.Fatal("程序、.old 与空的安装目录应被后台删除")
	}
}

func TestRemoveExecutableKeepsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sysbox.exe")
	other := filepath.Join(dir, "other.exe")
	for _, f := range []string{exe, other} {
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeExecutable(exe); err != nil {
		t.Fatal(err)
	}
	if !waitGone(t, exe) {
		t.Fatal("程序应被后台删除")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("安装目录中的其他文件不应被删除")
	}
}
