package fsx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 取消后立即停止遍历，不再累计大小
func TestDiskUsageContextCanceled(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if u := DiskUsageContext(context.Background(), dir); u.Bytes == 0 || u.Partial() {
		t.Fatalf("正常统计应有大小且完整：%+v", u)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if u := DiskUsageContext(ctx, dir); u.Bytes != 0 {
		t.Fatalf("取消后不应继续统计：%+v", u)
	}
}

// 无权限读取的子目录计入跳过数，结果标记为不完整
func TestDiskUsageContextSkipped(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要非 root 的类 Unix 权限模型")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f"), make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if u := DiskUsageContext(context.Background(), dir); !u.Partial() {
		t.Fatalf("无权限的目录应计入跳过数：%+v", u)
	}
}
