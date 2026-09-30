package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
)

// 复现：大小统计在后台按扫描顺序回填，界面却先按分类和大小重排了条目。
func TestMeasuredSizeFollowsItemAfterSort(t *testing.T) {
	src := stubSource{items: []cleanup.Item{
		{Path: "/cache/small", Name: "small", Category: 0, Selectable: true, Selected: true},
		{Path: "/cache/large", Name: "large", Category: 0, Selectable: true, Selected: true},
	}}
	p := NewClean(src, Env{}).(*cleanPage)
	p.Update(cleanScannedMsg{src.items})
	// 大的那项先统计完，下标是扫描时的 1
	p.Update(cleanMeasuredMsg{idx: 1, usage: fsx.Usage{Bytes: 900}})
	p.Update(cleanMeasuredMsg{idx: 0, usage: fsx.Usage{Bytes: 100}})
	p.Update(cleanScanDone{})

	if p.items[0].Name != "large" || p.items[0].Size != 900 {
		t.Fatalf("排序后第一项应是 large/900，实际 %s/%d", p.items[0].Name, p.items[0].Size)
	}
	if p.items[1].Name != "small" || p.items[1].Size != 100 {
		t.Fatalf("排序后第二项应是 small/100，实际 %s/%d", p.items[1].Name, p.items[1].Size)
	}
}

// 演练模式按路径前缀扣减清理后的大小。Windows 根目录用反斜杠，不能只认 '/'。
func TestEstimateAfterMatchesWindowsPath(t *testing.T) {
	src := stubSource{roots: []cleanup.Root{{Label: "缓存", Path: `C:\Users\me\AppData\Local\JetBrains`}}}
	p := NewClean(src, Env{DryRun: true}).(*cleanPage)
	p.before = []int64{1000}
	p.logs = []cleanLog{{item: cleanup.Item{
		Path: `C:\Users\me\AppData\Local\JetBrains\IntelliJIdea\index`,
		Size: 400,
	}}}
	after := p.estimateAfter()
	if after[0] != 600 {
		t.Fatalf("清理后应为 600，实际 %d", after[0])
	}
}

type stubSource struct {
	items []cleanup.Item
	roots []cleanup.Root
}

func (s stubSource) Title() string                  { return "测试" }
func (s stubSource) Categories() []cleanup.Category { return categories() }
func (s stubSource) Scan(func(string)) ([]cleanup.Item, error) {
	return append([]cleanup.Item(nil), s.items...), nil
}
func (s stubSource) Roots() []cleanup.Root  { return s.roots }
func (s stubSource) Check() *cleanup.Notice { return nil }
func (s stubSource) Remove(cleanup.Item) error {
	return nil
}
func (s stubSource) Notes() []string { return nil }
func (s stubSource) Tip() string     { return "" }

func categories() []cleanup.Category {
	return []cleanup.Category{{Label: "可清理", Primary: true}}
}

// 保证 tea 消息类型被引用，避免测试文件在裁剪时失效
var _ tea.Msg = cleanScanDone{}
