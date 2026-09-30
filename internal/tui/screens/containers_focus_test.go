package screens

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

func focusTestItems(n int) []containers.Item {
	var items []containers.Item
	for i := 0; i < n; i++ {
		item := containers.Item{ID: fmt.Sprint(i), Name: fmt.Sprintf("resource-%d", i), State: "running"}
		for j := 0; j < 80; j++ {
			item.Fields = append(item.Fields, containers.Field{Label: "信息", Value: fmt.Sprintf("详细信息 %d-%d", i, j)})
		}
		items = append(items, item)
	}
	return items
}
func TestResourceTabsAndPanelFocusAreIndependent(t *testing.T) {
	for _, kubernetes := range []bool{false, true} {
		p := newResourcePage(Env{}, kubernetes)
		p.items = focusTestItems(3)
		p.Body(120, 30)
		if p.focus != resourceFocusLeft || !reflect.DeepEqual(p.panelBorder(resourceFocusLeft), theme.Accent) || !reflect.DeepEqual(p.panelBorder(resourceFocusRight), theme.Faint) {
			t.Fatal("默认应高亮左侧列表边框")
		}
		if cmd := pressResource(p, "right"); cmd != nil || p.tab != 0 || p.cursor != 0 || p.focus != resourceFocusRight {
			t.Fatal("右键只应切换面板焦点，不能切换资源或触发查询")
		}
		if !reflect.DeepEqual(p.panelBorder(resourceFocusRight), theme.Accent) || !reflect.DeepEqual(p.panelBorder(resourceFocusLeft), theme.Faint) {
			t.Fatal("右侧焦点应高亮右框并取消左框高亮")
		}
		pressResource(p, "down")
		p.Body(120, 30)
		if p.output.YOffset != 1 || p.cursor != 0 || p.tab != 0 {
			t.Fatal("右侧下键应只滚动内容")
		}
		pressResource(p, "up")
		if p.output.YOffset != 0 || p.cursor != 0 {
			t.Fatal("右侧上键应只滚动内容")
		}
		pressResource(p, "down")
		pressResource(p, "left")
		if p.output.YOffset != 1 || p.focus != resourceFocusLeft || p.tab != 0 {
			t.Fatal("切换焦点应保留阅读位置和标签")
		}
		pressResource(p, "down")
		if p.cursor != 1 || p.focus != resourceFocusLeft {
			t.Fatal("左侧下键应选择下一项")
		}
		cmd := pressResource(p, "tab")
		if cmd == nil || p.tab != 1 || p.focus != resourceFocusLeft {
			t.Fatal("Tab 应切换标签并回到列表")
		}
		p.Update(resourceLoadedMsg{items: focusTestItems(2)})
		pressResource(p, "shift+tab")
		if p.tab != 0 {
			t.Fatal("Shift+Tab 应反向切换标签")
		}
		hints := p.resourceTabs(120, 30)
		if strings.Contains(hints, "←→") || !strings.Contains(hints, "Tab") {
			t.Fatal("标签提示只能使用 Tab 切换")
		}
		p.Close()
	}
}
func TestResourcePagingTargetsFocusedPanel(t *testing.T) {
	p := newResourcePage(Env{}, true)
	defer p.Close()
	p.items = focusTestItems(50)
	p.Body(120, 30)
	rows := p.listRows
	pressResource(p, "pgdown")
	if p.cursor != rows || p.output.YOffset != 0 {
		t.Fatal("左侧翻页应移动列表选择，不应滚动详情")
	}
	p.Body(120, 30)
	pressResource(p, "right")
	pressResource(p, "pgdown")
	if p.cursor != rows || p.output.YOffset == 0 {
		t.Fatal("右侧翻页应滚动详情，不应改变选择")
	}
	offset := p.output.YOffset
	pressResource(p, "left")
	if p.output.YOffset != offset {
		t.Fatal("切回左侧应保留详情位置")
	}
	pressResource(p, "/")
	pressResource(p, "pgdown")
	if p.cursor <= rows || p.mode != resourceInput {
		t.Fatal("筛选输入期间翻页应继续操作左侧列表")
	}
	pressResource(p, "enter")
	pressResource(p, "f")
	p.Body(120, 30)
	pressResource(p, "down")
	if p.focus != resourceFocusRight || p.output.YOffset != 1 || p.mode != resourceInput {
		t.Fatal("右侧查询时上下键仍应滚动信息")
	}
}
func TestResourceFocusChangeKeepsLiveLogs(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	p.items = focusTestItems(2)
	p.pane = "logs"
	p.logAutoScroll = true
	p.logAlive = true
	p.logSeq = 1
	p.focus = resourceFocusRight
	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("实时日志 %d", i))
	}
	p.Update(resourceLogMsg{seq: 1, lines: lines})
	p.Body(120, 30)
	bottom := p.output.YOffset
	pressResource(p, "up")
	p.Body(120, 30)
	if p.logAutoScroll || !p.logAlive || p.output.YOffset != bottom-1 || p.cursor != 0 {
		t.Fatal("右侧滚动应暂停跟随，保持连接和资源选择")
	}
	pressResource(p, "left")
	if !p.logAlive || p.tab != 0 {
		t.Fatal("只切换面板不能关闭日志流")
	}
	pressResource(p, "right")
	pressResource(p, " ")
	p.Body(120, 30)
	if !p.logAutoScroll || !p.output.AtBottom() {
		t.Fatal("重新进入右侧后应能恢复自动跟随")
	}
	pressResource(p, "left")
	pressResource(p, "down")
	if p.logAlive || p.pane != "info" || p.cursor != 1 {
		t.Fatal("左侧选择其他资源应关闭旧日志并显示新详情")
	}
}
