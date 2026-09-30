package screens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/config"
)

// 首页汇总只统计默认勾选的条目，数据源出错时显示不可用，旧批次结果被丢弃
func TestHomeScanSummary(t *testing.T) {
	ok := stubSource{items: []cleanup.Item{
		{Path: "/a", Selectable: true, Selected: true, Sized: true, Size: 300},
		{Path: "/b", Selectable: true, Selected: false, Sized: true, Size: 900},
	}}
	tools := []Tool{
		{ID: "ok", Name: "ok", Source: func(Env) (cleanup.Source, error) { return ok, nil }},
		{ID: "bad", Name: "bad", Source: func(Env) (cleanup.Source, error) { return nil, errors.New("找不到") }},
		{ID: "plain", Name: "plain"},
	}
	h := NewHome(nil, tools, nil, Env{})
	h.scanAll()
	if len(h.sums) != 2 {
		t.Fatalf("只有清理类工具参与扫描，实际 %d", len(h.sums))
	}
	for _, tl := range tools[:2] {
		h.Update(scanTool(context.Background(), tl, Env{}, h.gen)())
	}
	if s := h.sums["ok"]; !s.done || s.n != 1 || s.size != 300 {
		t.Fatalf("应只统计默认勾选的 300 B：%+v", s)
	}
	if s := h.sums["bad"]; !s.done || s.failure == "" {
		t.Fatalf("出错的工具应标记不可用：%+v", s)
	}
	// 重新扫描后，上一批次迟到的结果不能覆盖新状态
	old := h.gen
	h.scanAll()
	h.Update(homeSumMsg{id: "ok", gen: old, n: 9, size: 9999})
	if h.sums["ok"].done {
		t.Fatal("旧批次的结果应被丢弃")
	}
}

// 从首页打开正在扫描的工具时取消首页扫描，被取消的扫描不回传结果，返回首页后重新扫描
func TestHomeOpenCancelsRunningScan(t *testing.T) {
	src := stubSource{items: []cleanup.Item{{Path: "/a", Selectable: true, Selected: true, Sized: true, Size: 1}}}
	tools := []Tool{{ID: "ok", Name: "ok", Source: func(Env) (cleanup.Source, error) { return src, nil }}}
	h := NewHome(nil, tools, nil, Env{})
	h.scanAll()
	running := h.sums["ok"]
	h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if running.done {
		t.Fatal("取消后不应标记为完成")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if msg := scanTool(ctx, tools[0], Env{}, h.gen)(); msg != nil {
		t.Fatalf("已取消的扫描不应回传结果：%+v", msg)
	}
	if h.Refresh("ok") == nil || h.sums["ok"] == running {
		t.Fatal("返回首页后应重新扫描被取消的工具")
	}
}

// 有条目统计不完整时，工具和首页总量都标记为“至少”
func TestHomePartialSummary(t *testing.T) {
	src := stubSource{items: []cleanup.Item{{Path: "/a", Selectable: true, Selected: true, Sized: true, Size: 5, Partial: true}}}
	tools := []Tool{{ID: "ok", Name: "ok", Source: func(Env) (cleanup.Source, error) { return src, nil }}}
	h := NewHome(nil, tools, nil, Env{})
	h.scanAll()
	h.Update(scanTool(context.Background(), tools[0], Env{}, h.gen)())
	if text, _ := sumText(h.sums["ok"]); !strings.Contains(text, "≥") {
		t.Fatalf("统计不完整时应显示 ≥，实际 %q", text)
	}
	if _, _, partial := h.total(); !partial {
		t.Fatal("总量应标记为不完整")
	}
}

// ←→ 切换分类，↑↓ 只在当前分类内移动，切回分类时保留原来的选中项
func TestHomeCategoryNavigation(t *testing.T) {
	tools := []Tool{
		{ID: "a1", Group: "A"}, {ID: "b1", Group: "B"}, {ID: "a2", Group: "A"}, {ID: "b2", Group: "B"},
	}
	h := NewHome(nil, tools, nil, Env{})
	if len(h.cats) != 2 || h.cats[0].Name != "A" {
		t.Fatalf("分类应按出现顺序合并：%+v", h.cats)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cur, _ := h.current(); cur.tool.ID != "a2" {
		t.Fatalf("↓ 应在分类内移动到 a2，实际 %s", cur.tool.ID)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cur, _ := h.current(); cur.tool.ID != "b1" {
		t.Fatalf("→ 应切到 B 分类的 b1，实际 %s", cur.tool.ID)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyUp})
	if cur, _ := h.current(); cur.tool.ID != "b2" {
		t.Fatalf("↑ 应在分类内循环到 b2，实际 %s", cur.tool.ID)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cur, _ := h.current(); cur.tool.ID != "a2" {
		t.Fatalf("切回 A 分类应保留 a2，实际 %s", cur.tool.ID)
	}
	if msg := h.Update(tea.KeyMsg{Type: tea.KeyEnter})(); msg != (OpenMsg{"a2"}) {
		t.Fatalf("enter 应打开当前工具：%+v", msg)
	}
}

// 固定列出的分类即使没有工具也显示，进入后上下移动和 enter 都不做任何事
func TestHomeEmptyCategory(t *testing.T) {
	h := NewHome([]Group{{Name: "A"}, {Name: "空"}}, []Tool{{ID: "a1", Group: "A"}, {ID: "x", Group: "X"}}, nil, Env{})
	if len(h.cats) != 3 || h.cats[1].Name != "空" || h.cats[2].Name != "X" {
		t.Fatalf("应先按固定顺序列出分类，再追加未登记的分组：%+v", h.cats)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	h.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("空分类按 enter 不应打开任何页面")
	}
	if body := h.Body(120, 40); !strings.Contains(body, "即将推出") {
		t.Fatal("空分类应显示即将推出")
	}
}

// 选项型设置按 enter 切到下一个值并通知根模型；文字型设置在输入框里校验，不合法时不保存
func TestHomeSettings(t *testing.T) {
	toggle := Setting{ID: "t", Group: "设置", Options: []string{"on", "off"},
		Get: func(env Env) string {
			if env.Config.Update.DisableCheck {
				return "off"
			}
			return "on"
		},
		Set: func(c *config.Config, v string) error { c.Update.DisableCheck = v == "off"; return nil }}
	text := Setting{ID: "p", Group: "设置",
		Get: func(env Env) string { return env.Config.Claude.Proxy },
		Set: func(c *config.Config, v string) error {
			if v == "bad" {
				return errors.New("不合法")
			}
			c.Claude.Proxy = v
			return nil
		}}
	h := NewHome([]Group{{Name: "设置"}}, nil, []Setting{toggle, text}, Env{})
	msg, ok := h.Update(tea.KeyMsg{Type: tea.KeyEnter})().(ConfigMsg)
	if !ok || !h.env.Config.Update.DisableCheck {
		t.Fatalf("enter 应切换为 off 并发出 ConfigMsg：%+v", h.env.Config)
	}
	var c config.Config
	msg.Edit(&c)
	if !c.Update.DisableCheck {
		t.Fatal("ConfigMsg 应携带同样的修改")
	}

	h.Update(tea.KeyMsg{Type: tea.KeyDown})
	h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !h.Typing() {
		t.Fatal("文字型设置按 enter 应进入输入状态")
	}
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad")})
	if cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || h.editErr == "" || !h.Typing() {
		t.Fatal("不合法的输入应提示错误并留在输入框")
	}
	h.input.SetValue("http://127.0.0.1:1080")
	if cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || h.Typing() || h.env.Config.Claude.Proxy != "http://127.0.0.1:1080" {
		t.Fatalf("合法输入应保存并退出输入框：%+v", h.env.Config.Claude)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	h.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if h.Typing() || h.env.Config.Claude.Proxy != "http://127.0.0.1:1080" {
		t.Fatal("esc 应取消编辑且不修改配置")
	}
}

// 过渡动画按 8 毫秒一帧推进时，顶部每帧最多移动一行；首帧是首页，末帧是分类条加工具页
func TestSlideMovesOneLinePerFrame(t *testing.T) {
	groups := []Group{{Name: "A", Icon: []string{"pp", "pp"}}, {Name: "B"}}
	tools := []Tool{{ID: "a", Group: "A", Name: "a"}, {ID: "b", Group: "B", Name: "b"}}
	h := NewHome(groups, tools, nil, Env{})
	for _, height := range []int{24, 40, 60} {
		page := func(w, h int) string { return strings.Repeat("page\n", h-1) + "page" }
		s := h.NewSlide(150, height, true, page)
		const frame = 8 * time.Millisecond
		dur := s.Duration(frame)
		prev := -1
		for at := time.Duration(0); ; at += frame {
			p := min(1, float64(at)/float64(dur))
			out := s.Frame(p)
			if lipgloss.Height(out) != height {
				t.Fatalf("高度 %d：帧高度应保持 %d，实际 %d", height, height, lipgloss.Height(out))
			}
			// 顶部行数 = 总高度减去工具页行数
			top := height - strings.Count(out, "page")
			if prev >= 0 && prev-top > 1 {
				t.Fatalf("高度 %d：一帧移动了 %d 行", height, prev-top)
			}
			prev = top
			if p == 1 {
				break
			}
		}
		if prev != lipgloss.Height(s.strip) {
			t.Fatalf("末帧顶部应是分类条，实际 %d 行", prev)
		}
	}
}
