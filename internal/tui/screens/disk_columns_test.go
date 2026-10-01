package screens

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/tools/disk"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func loadDiskTest(t *testing.T, p *diskPage) {
	t.Helper()
	result, err := p.spec.load(context.Background(), p.tab, p.target, false)
	if err != nil {
		t.Fatal(err)
	}
	p.Update(systemLoadedMsg{generation: p.generation, key: p.cacheKey(), result: result})
}
func TestDiskInitialDirectory(t *testing.T) {
	p := NewDisk(Env{}).(*diskPage)
	defer p.Close()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if p.target != cwd || p.tab != 0 {
		t.Fatal("默认应扫描启动目录并显示目录占用")
	}
}
func TestDiskThreeColumnsAndReturn(t *testing.T) {
	root := t.TempDir()
	path := root
	for _, name := range []string{"第一层", "第二层", "第三层", "第四层"} {
		path = filepath.Join(path, name)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	p := NewDisk(Env{}).(*diskPage)
	defer p.Close()
	p.target = root
	loadDiskTest(t, p)
	for i := 0; i < 4; i++ {
		p.Update(tea.KeyMsg{Type: tea.KeyRight})
		loadDiskTest(t, p)
	}
	body := ansi.Strip(p.Body(150, 32))
	if strings.Contains(body, root+" ·") || !strings.Contains(body, "第三层") || !strings.Contains(body, "第四层") {
		t.Fatal("应只显示最近三层目录")
	}
	if strings.Contains(body, "基本信息") || strings.Contains(body, "字段") {
		t.Fatal("不应显示详情面板")
	}
	for i := 0; i < 4; i++ {
		p.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	if p.target != root || len(p.levels) != 0 {
		t.Fatal("移出屏幕的目录仍应能逐层返回")
	}
	p.Update(systemKey("g"))
	p.input.SetValue(path)
	p.Update(systemKey("enter"))
	loadDiskTest(t, p)
	if len(p.levels) != 0 || p.target != path {
		t.Fatal("自定义目录应成为新的分栏起点")
	}
}
func TestDiskSortPreservesSelection(t *testing.T) {
	root := t.TempDir()
	for name, size := range map[string]int{"a": 8192, "b": 16384, "c": 4096} {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := NewDisk(Env{}).(*diskPage)
	defer p.Close()
	p.target = root
	loadDiskTest(t, p)
	if p.visible[0].name != "b" {
		t.Fatal("默认应按大小降序")
	}
	p.cursor = 1
	selected, _ := p.current()
	p.Update(systemKey("n"))
	current, _ := p.current()
	if p.visible[0].name != "a" || selected.id() != current.id() {
		t.Fatal("名称排序应保留选中项")
	}
	p.Update(systemKey("n"))
	if p.visible[0].name != "c" {
		t.Fatal("再次名称排序应切换降序")
	}
	p.Update(systemKey("s"))
	if p.visible[0].name != "b" {
		t.Fatal("切换大小排序应按降序")
	}
}

func TestDiskRemovalUpdatesParents(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "子目录")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(child, "文件")
	if err := os.WriteFile(file, make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewDisk(Env{}).(*diskPage)
	defer p.Close()
	p.target = root
	loadDiskTest(t, p)
	p.Update(tea.KeyMsg{Type: tea.KeyRight})
	loadDiskTest(t, p)
	before := p.state.tree.Root.Bytes
	// 仅更新扫描缓存，验证删除后的显示；测试不调用系统废纸篓。
	p.state.tree.Remove(file)
	p.Update(systemActedMsg{change: true, reload: true})
	loadDiskTest(t, p)
	p.Update(tea.KeyMsg{Type: tea.KeyLeft})
	node := p.visible[0].value.(*disk.Node)
	if p.state.tree.Root.Bytes >= before || node.Files != 0 || p.visible[0].cells[0] != fsx.FormatBytes(node.Bytes) {
		t.Fatal("返回上层时应显示删除后的占用")
	}
}
