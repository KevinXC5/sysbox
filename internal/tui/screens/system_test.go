package screens

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func systemKey(key string) tea.KeyMsg {
	if key == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	if key == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
func TestSystemStaleResultsAndSearch(t *testing.T) {
	page := newSystemPage(Env{}, systemSpec{name: "测试", tabs: []string{"列表"}, load: func(context.Context, int, string) (systemResult, error) { return systemResult{}, nil }})
	defer page.Close()
	page.reload()
	page.Update(systemLoadedMsg{generation: page.generation - 1, result: systemResult{rows: []systemRow{{name: "old"}}}})
	if len(page.visible) != 0 {
		t.Fatal("过期查询结果不能覆盖当前列表")
	}
	page.Update(systemLoadedMsg{generation: page.generation, result: systemResult{rows: []systemRow{{name: "alpha", lines: []string{"端口 8080"}}, {name: "beta"}}}})
	page.filter = "8080"
	page.rebuild()
	if len(page.visible) != 1 || page.visible[0].name != "alpha" {
		t.Fatal("应能按详情字段查询")
	}
}
func TestSystemActionConfirmationAndDryRun(t *testing.T) {
	calls := 0
	action := systemAction{key: "x", label: "停止", preview: "stop agent", mutates: true, run: func(context.Context) (string, error) { calls++; return "完成", nil }}
	for _, dry := range []bool{false, true} {
		page := newSystemPage(Env{DryRun: dry}, systemSpec{name: "测试", tabs: []string{"列表"}, actions: func(int, systemRow) []systemAction { return []systemAction{action} }})
		page.visible = []systemRow{{name: "agent"}}
		cmd := page.Update(systemKey("x"))
		if cmd != nil || page.confirm == nil {
			t.Fatal("服务修改必须先确认")
		}
		page.Update(systemKey("esc"))
		if page.confirm != nil || calls != 0 {
			t.Fatal("取消确认不能执行操作")
		}
		page.Update(systemKey("x"))
		cmd = page.Update(systemKey("y"))
		if dry {
			if cmd != nil || !strings.Contains(page.note, "演练模式") {
				t.Fatal("演练确认后只能显示命令")
			}
		} else {
			if cmd == nil {
				t.Fatal("确认后应提供执行命令")
			}
			page.Update(cmd())
			calls = 0
		}
		page.Close()
	}
}
func TestSystemRenderingStripsControlSequences(t *testing.T) {
	page := newSystemPage(Env{}, systemSpec{name: "测试", tabs: []string{"列表"}})
	defer page.Close()
	page.visible = []systemRow{{name: "test", lines: []string{"\x1b[2J正文"}}}
	body := ansi.Strip(page.Body(100, 26))
	if strings.Contains(body, "\x1b") || !strings.Contains(body, "正文") {
		t.Fatal("外部内容必须过滤终端控制序列")
	}
}

func TestSystemNarrowDetail(t *testing.T) {
	page := newSystemPage(Env{}, systemSpec{name: "测试", tabs: []string{"列表"}})
	defer page.Close()
	page.visible = []systemRow{{name: "条目", lines: []string{"完整详情"}}}
	page.focus = 1
	if !strings.Contains(ansi.Strip(page.Body(60, 16)), "完整详情") {
		t.Fatal("窄终端也应能查看详情")
	}
}

func TestSystemSearchRanksExactNameFirst(t *testing.T) {
	page := newSystemPage(Env{}, systemSpec{name: "测试", tabs: []string{"列表"}})
	defer page.Close()
	page.result.rows = []systemRow{{name: "before", lines: []string{"agent 信息"}}, {name: "agent"}, {name: "ag-next-entry"}}
	page.filter = "agent"
	page.rebuild()
	if len(page.visible) == 0 || page.visible[0].name != "agent" {
		t.Fatal("搜索完整名称时应先选中准确匹配的条目")
	}
}
