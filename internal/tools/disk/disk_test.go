package disk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/KevinXC5/sysbox/internal/fsx"
)

func TestScanRanksAndSkipsLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "artifact")
	if e := os.WriteFile(file, make([]byte, 8192), 0600); e != nil {
		t.Fatal(e)
	}
	external := t.TempDir()
	if e := os.WriteFile(filepath.Join(external, "private"), make([]byte, 32768), 0600); e != nil {
		t.Fatal(e)
	}
	// Windows 未授权创建符号链接时，仍验证普通目录统计。
	_ = os.Symlink(external, filepath.Join(root, "link"))
	report, e := Scan(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Entries) != 1 || report.Entries[0].Path != dir || len(report.Files) != 1 || report.Files[0].Path != file {
		t.Fatalf("目录或大文件范围错误：%+v", report)
	}
	info, e := os.Stat(file)
	if e != nil {
		t.Fatal(e)
	}
	if report.Entries[0].Bytes < fsx.AllocSize(info) {
		t.Fatal("目录占用没有包含子文件")
	}
}
func TestTopFilesAndCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d", i)), make([]byte, (i+1)*4096), 0600); e != nil {
			t.Fatal(e)
		}
	}
	report, e := Scan(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Files) != 50 || filepath.Base(report.Files[0].Path) != "f59" {
		t.Fatal("应只保留最大的 50 个文件")
	}
	for i := 1; i < len(report.Files); i++ {
		if report.Files[i].Bytes > report.Files[i-1].Bytes {
			t.Fatal("大文件应按大小降序排列")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Scan(ctx, root); !errors.Is(e, context.Canceled) {
		t.Fatalf("扫描未正确取消：%v", e)
	}
}
func TestCleanupRouting(t *testing.T) {
	if CleanupTool("/repo/node_modules") != "projects" || CleanupTool("/home/.codex/cache") != "agent" || CleanupTool("/home/Documents") == "devcache" {
		t.Fatal("清理入口应匹配已知路径")
	}
}
