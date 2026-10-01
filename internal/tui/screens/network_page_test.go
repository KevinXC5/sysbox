package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/tools/network"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func testNetworkPage(t *testing.T) *networkPage {
	t.Helper()
	p := NewNetwork(Env{}).(*networkPage)
	t.Cleanup(p.Close)
	p.checker = func(_ context.Context, target string, _ network.Via) network.Report {
		return network.Report{Target: target, Address: target, Status: "200 OK", Route: "直连", Total: time.Millisecond}
	}
	return p
}
func TestNetworkFullTableAndOptionalDetails(t *testing.T) {
	p := testNetworkPage(t)
	target := network.Presets[0].Target
	cmd := p.startTarget(target)
	p.Update(cmd())
	body := ansi.Strip(p.Body(150, 32))
	if strings.Contains(body, "解析结果") || !strings.Contains(body, "失败环节") || !strings.Contains(body, "检测时间") {
		t.Fatal("默认应显示全宽结果表格，不展开详情")
	}
	p.Update(systemKey("enter"))
	if !p.detail || !strings.Contains(ansi.Strip(p.Body(150, 32)), target) {
		t.Fatal("回车应展开当前目标详情")
	}
	cursor := p.cursor
	offset := p.offset
	p.Update(systemKey("esc"))
	if p.detail || p.cursor != cursor || p.offset != offset {
		t.Fatal("关闭详情应保留列表位置")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRight})
	if p.focus != 0 {
		t.Fatal("表格不应切换到不存在的右侧面板")
	}
}
func TestNetworkIndependentResultsAndStaleRetest(t *testing.T) {
	p := testNetworkPage(t)
	a, b := network.Presets[0].Target, network.Presets[1].Target
	first := p.startTarget(a)
	p.startTarget(b)
	old := first()
	newer := p.startTarget(a)
	p.Update(old)
	if _, ok := p.state.reports[a]; ok {
		t.Fatal("旧检测不得覆盖新一轮检测")
	}
	p.Update(newer())
	if _, ok := p.state.reports[a]; !ok || !p.state.pending[b] || p.loading {
		t.Fatal("慢目标不能阻塞已完成目标")
	}
	generation := p.checks[b].generation
	p.Update(systemKey("esc"))
	if len(p.checks) != 0 || p.state.reports[b].Err != context.Canceled {
		t.Fatal("取消检测应保留完成结果并标记未完成项")
	}
	p.Update(networkCheckedMsg{target: b, generation: generation, report: network.Report{Status: "200 OK"}})
	if p.state.reports[b].Err != context.Canceled {
		t.Fatal("取消后的结果不能覆盖取消状态")
	}
}
func TestNetworkCompareUsesSelectedTarget(t *testing.T) {
	p := testNetworkPage(t)
	p.syncTargets()
	p.cursor = 2
	selected, _ := p.current()
	cursor, offset := p.cursor, p.offset
	p.Update(systemKey("p"))
	if p.compare == nil || !strings.Contains(p.compare.result.info, selected.key) {
		t.Fatal("应直接对比当前目标")
	}
	p.Update(systemKey("esc"))
	if p.compare != nil || p.cursor != cursor || p.offset != offset {
		t.Fatal("关闭线路对比应保留原列表位置")
	}
	p.Update(networkComparedMsg{generation: p.compareGeneration - 1, result: systemResult{info: "过期"}})
	if p.compare != nil {
		t.Fatal("已关闭对比不应被异步结果重新打开")
	}
}
func TestNetworkRemovePendingCustomTarget(t *testing.T) {
	p := testNetworkPage(t)
	target := "https://example.test"
	p.state.add(target)
	cmd := p.startTarget(target)
	p.cursor = 0
	msg := cmd()
	p.Update(systemKey("x"))
	p.Update(msg)
	if p.state.isCustom(target) || p.state.pending[target] {
		t.Fatal("移除目标应撤销未完成的检测")
	}
	if _, ok := p.state.reports[target]; ok {
		t.Fatal("移除后的异步结果不能恢复目标")
	}
}

func TestNetworkAsyncLoadKeepsDetailFocus(t *testing.T) {
	p := testNetworkPage(t)
	p.tab = 1
	row := systemRow{name: "状态", key: "status"}
	p.result = systemResult{rows: []systemRow{row}}
	p.rebuild(false)
	p.Update(systemKey("enter"))
	p.Update(systemLoadedMsg{generation: p.generation, result: systemResult{rows: []systemRow{row}}})
	if !p.detail || p.focus != 1 {
		t.Fatal("后台更新不能把详情滚动焦点切回表格")
	}
}

func TestNetworkCompareRowsAndNavigation(t *testing.T) {
	p := testNetworkPage(t)
	p.syncTargets()
	p.Update(systemKey("p"))
	p.Update(networkComparedMsg{generation: p.compareGeneration, result: systemResult{info: "对比目标：https://example.test · 按 g 更换", rows: []systemRow{
		{name: "结论", key: "conclusion"},
		{name: "系统 HTTP", key: "system-http"},
		{name: "检测：直连", key: "check-直连", value: network.Report{Address: "https://example.test", Route: "直连", Status: "200 OK"}},
		{name: "检测：经系统代理", key: "check-经系统代理", cells: []string{"跳过", "未开启"}},
		{name: "检测：按终端设置", key: "check-按终端设置", cells: []string{"跳过", "未设置"}},
	}}})
	if len(p.compare.visible) != 3 || strings.Contains(p.compare.result.info, "按 g") {
		t.Fatal("对比应只显示三条线路和可用操作")
	}
	p.Update(systemKey("enter"))
	if p.compare.focus != 1 {
		t.Fatal("应能打开选中线路详情")
	}
	p.Update(systemKey("esc"))
	if p.compare == nil || p.compare.focus != 0 {
		t.Fatal("关闭链路详情应返回线路对比表格")
	}
	p.Update(systemKey("esc"))
	if p.compare != nil {
		t.Fatal("再次关闭应返回诊断表格")
	}
}
