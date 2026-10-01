package screens

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
)

func systemKey(key string) tea.KeyMsg {
	switch key {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func staticSpec(rows ...systemRow) systemSpec {
	return systemSpec{name: "测试", tabs: []string{"列表"}, timeout: 1, load: func(context.Context, int, string, bool) (systemResult, error) {
		return systemResult{rows: rows}, nil
	}}
}

func TestSystemStaleResultsAndSearch(t *testing.T) {
	page := newSystemPage(Env{}, staticSpec())
	defer page.Close()
	page.reload(false)
	page.Update(systemLoadedMsg{generation: page.generation - 1, result: systemResult{rows: []systemRow{{name: "old"}}}})
	if len(page.visible) != 0 {
		t.Fatal("过期查询结果不能覆盖当前列表")
	}
	page.Update(systemLoadedMsg{generation: page.generation, result: systemResult{rows: []systemRow{{name: "alpha", fields: []containers.Field{{Label: "端口", Value: "8080"}}}, {name: "beta"}}}})
	page.filter = "8080"
	page.rebuild(false)
	if len(page.visible) != 1 || page.visible[0].name != "alpha" {
		t.Fatal("应能按详情字段查询")
	}
}

func TestSystemTabSwitchUsesCache(t *testing.T) {
	loads := 0
	spec := systemSpec{name: "测试", tabs: []string{"一", "二"}, timeout: 1, load: func(_ context.Context, tab int, _ string, _ bool) (systemResult, error) {
		loads++
		return systemResult{rows: []systemRow{{name: []string{"甲", "乙"}[tab]}}}, nil
	}}
	page := newSystemPage(Env{}, spec)
	defer page.Close()
	run := func(cmd tea.Cmd) {
		if cmd != nil {
			page.Update(cmd())
		}
	}
	run(page.Init())
	run(page.Update(systemKey("tab")))
	run(page.Update(systemKey("tab")))
	if loads != 2 || page.visible[0].name != "甲" {
		t.Fatalf("切回已读取的类别应直接使用缓存，实际读取 %d 次", loads)
	}
	run(page.Update(systemKey("r")))
	if loads != 3 {
		t.Fatal("按 r 应重新读取")
	}
}

func TestSystemViewsAndCursorKeepsIdentity(t *testing.T) {
	spec := staticSpec()
	spec.views = []string{"偶数", "全部"}
	spec.view = func(view int, row systemRow) bool { return view == 1 || row.key != "b" }
	page := newSystemPage(Env{}, spec)
	defer page.Close()
	page.result = systemResult{rows: []systemRow{{name: "a", key: "a"}, {name: "b", key: "b"}, {name: "c", key: "c"}}}
	page.rebuild(false)
	if len(page.visible) != 2 {
		t.Fatal("视图应过滤条目")
	}
	page.cursor = 1
	page.Update(systemKey("f"))
	if len(page.visible) != 3 || page.visible[page.cursor].key != "c" {
		t.Fatal("切换视图后应保持选中同一条目")
	}
}

func TestSystemEscCancelsUnboundedLoad(t *testing.T) {
	spec := staticSpec()
	spec.timeout = 0
	page := newSystemPage(Env{}, spec)
	defer page.Close()
	cmd := page.reload(false)
	if page.Update(systemKey("esc")) != nil || !page.loading {
		t.Fatal("读取中按 esc 应先取消读取而不是返回")
	}
	page.Update(cmd())
	if page.loading {
		t.Fatal("读取结束后应恢复空闲")
	}
}

func TestSystemActionConfirmationAndDryRun(t *testing.T) {
	calls := 0
	action := systemAction{key: "x", label: "停止", preview: "stop agent", mutates: true, run: func(context.Context) (string, error) { calls++; return "完成", nil }}
	for _, dry := range []bool{false, true} {
		spec := staticSpec()
		spec.actions = func(int, systemRow) []systemAction { return []systemAction{action} }
		page := newSystemPage(Env{DryRun: dry}, spec)
		page.visible = []systemRow{{name: "agent"}}
		cmd := page.Update(systemKey("x"))
		if cmd != nil || page.confirm == nil {
			t.Fatal("修改必须先确认")
		}
		page.Update(systemKey("esc"))
		if page.confirm != nil || calls != 0 {
			t.Fatal("取消确认不能执行操作")
		}
		page.Update(systemKey("x"))
		cmd = page.Update(systemKey("y"))
		if dry {
			if cmd != nil || !strings.Contains(page.notice, "演练模式") {
				t.Fatal("演练确认后只能显示命令")
			}
		} else {
			if cmd == nil {
				t.Fatal("确认后应执行")
			}
			page.Update(cmd())
			if calls != 1 || page.notice != "完成" {
				t.Fatal("执行结果应显示在提示行")
			}
			calls = 0
		}
		page.Close()
	}
}

func TestSystemReadActionShowsOutput(t *testing.T) {
	spec := staticSpec()
	spec.actions = func(int, systemRow) []systemAction {
		return []systemAction{{key: "l", label: "日志", run: func(context.Context) (string, error) { return "第一行", nil }}}
	}
	page := newSystemPage(Env{}, spec)
	defer page.Close()
	page.visible = []systemRow{{name: "agent"}}
	cmd := page.Update(systemKey("l"))
	page.Update(cmd())
	if page.output != "第一行" || page.focus != 1 || !strings.Contains(ansi.Strip(page.Body(120, 30)), "第一行") {
		t.Fatal("查看类操作应在右侧面板显示输出")
	}
	page.Update(systemKey("esc"))
	if page.output != "" {
		t.Fatal("esc 应先关闭输出")
	}
}

func TestSystemRenderingStripsControlSequences(t *testing.T) {
	spec := staticSpec()
	spec.columns = func(int) []systemColumn { return []systemColumn{{title: "名称"}, {title: "状态", width: 8}} }
	page := newSystemPage(Env{}, spec)
	defer page.Close()
	page.visible = []systemRow{{name: "test", cells: []string{"\x1b[31m状态"}, fields: []containers.Field{{Label: "正文", Value: "\x1b[2J内容"}}}}
	body := ansi.Strip(page.Body(120, 26))
	if strings.Contains(body, "\x1b") || !strings.Contains(body, "内容") || !strings.Contains(body, "状态") {
		t.Fatal("外部内容必须过滤终端控制序列")
	}
}

func TestSystemNarrowDetail(t *testing.T) {
	page := newSystemPage(Env{}, staticSpec())
	defer page.Close()
	page.visible = []systemRow{{name: "条目", fields: []containers.Field{{Label: "说明", Value: "完整详情"}}}}
	page.focus = 1
	if !strings.Contains(ansi.Strip(page.Body(60, 16)), "完整详情") {
		t.Fatal("窄终端也应能查看详情")
	}
}

func TestSystemSearchRanksExactNameFirst(t *testing.T) {
	page := newSystemPage(Env{}, staticSpec())
	defer page.Close()
	page.result.rows = []systemRow{{name: "before", fields: []containers.Field{{Value: "agent 信息"}}}, {name: "agent"}, {name: "ag-next-entry"}}
	page.filter = "agent"
	page.rebuild(false)
	if len(page.visible) == 0 || page.visible[0].name != "agent" {
		t.Fatal("搜索完整名称时应先选中准确匹配的条目")
	}
}

func TestColumnWidthsDropColumnsWhenNarrow(t *testing.T) {
	cols := []systemColumn{{title: "名称"}, {title: "状态", width: 12}, {title: "PID", width: 7}}
	if w := columnWidths(cols, 60); len(w) != 3 || w[0] != 60-12-7-2 {
		t.Fatalf("弹性列应占满剩余宽度：%v", w)
	}
	if w := columnWidths(cols, 24); len(w) != 2 {
		t.Fatalf("宽度不足时应隐藏右侧列：%v", w)
	}
}
