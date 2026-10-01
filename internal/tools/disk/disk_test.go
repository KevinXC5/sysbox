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

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestScanBuildsTreeAndSkipsLinks(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "project", "src", "artifact")
	write(t, file, 8192)
	external := t.TempDir()
	write(t, filepath.Join(external, "private"), 32768)
	// Windows 未授权创建符号链接时，仍验证普通目录统计
	_ = os.Symlink(external, filepath.Join(root, "link"))
	tree, err := Scan(context.Background(), root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root.Children) != 1 || tree.Root.Files != 1 {
		t.Fatalf("不应跟随符号链接：%+v", tree.Root.Children)
	}
	project := tree.Find(filepath.Join(root, "project"))
	info, _ := os.Stat(file)
	if project == nil || project.Bytes < fsx.AllocSize(info) || tree.Find(file) == nil {
		t.Fatal("目录占用应包含子文件，并可直接定位子目录")
	}
	if len(tree.LargestUnder(filepath.Join(root, "project", "src"))) != 1 {
		t.Fatal("大文件应可按子目录筛选")
	}
}

func TestSmallFilesAreMergedAndRemoveUpdatesTotals(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < keepFiles+10; i++ {
		write(t, filepath.Join(root, fmt.Sprintf("f%02d", i)), (i+1)*4096)
	}
	tree, err := Scan(context.Background(), root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root.Rest != 10 || len(tree.Root.Children) != keepFiles {
		t.Fatalf("小文件应合并统计：保留 %d，合并 %d", len(tree.Root.Children), tree.Root.Rest)
	}
	if len(tree.Largest) != keepFiles+10 || filepath.Base(tree.Largest[0].Path) != fmt.Sprintf("f%02d", keepFiles+9) {
		t.Fatal("大文件排行应覆盖全部文件并按大小降序")
	}
	before := tree.Root.Bytes
	biggest := tree.Root.Children[0]
	tree.Remove(biggest.Path)
	smallest := tree.Largest[len(tree.Largest)-1]
	tree.Remove(smallest.Path)
	if tree.Root.Bytes != before-biggest.Bytes-smallest.Bytes || tree.Root.Rest != 9 || tree.Root.Files != keepFiles+8 {
		t.Fatal("移除后应同步更新目录占用与合并统计")
	}
}

func TestScanReusesSubtreeAndCancels(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "big"), 65536)
	write(t, filepath.Join(root, "b"), 4096)
	child, err := Scan(context.Background(), filepath.Join(root, "a"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	child.Root.Bytes = 1 << 40 // 标记：复用时不会重新统计
	tree, err := Scan(context.Background(), root, nil, child)
	if err != nil || tree.Find(filepath.Join(root, "a")).Bytes < 1<<40 {
		t.Fatal("返回上级目录时应复用已扫描的子目录")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tree, err = Scan(ctx, root, nil, nil)
	if !errors.Is(err, context.Canceled) || tree == nil || !tree.Partial {
		t.Fatalf("取消时应返回部分结果：%v", err)
	}
}

func TestCleanupRoutingAndTrashGuard(t *testing.T) {
	cases := map[string]string{"/repo/node_modules": "projects", "/home/.codex/cache": "agent", "/home/code/app": "", "/home/Documents/cache": "", "/home/go/pkg/mod/x": "devcache", "/Users/a/Library/Caches/JetBrains/IU": "jetbrains"}
	for path, want := range cases {
		if got := CleanupTool(path); got != want {
			t.Fatalf("%s 应对应 %q，实际 %q", path, want, got)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if Trashable(home) == nil || Trashable(filepath.Join(home, "Library")) == nil || Trashable(filepath.Dir(home)) == nil {
		t.Fatal("家目录、系统资料目录与家目录之外的路径不能移到废纸篓")
	}
	if Trashable(filepath.Join(home, "Downloads", "a.zip")) != nil {
		t.Fatal("家目录内的普通文件应允许移到废纸篓")
	}
}
