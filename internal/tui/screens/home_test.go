package screens

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/cleanup"
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
	h := NewHome(tools, Env{})
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
	h := NewHome(tools, Env{})
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
	h := NewHome(tools, Env{})
	h.scanAll()
	h.Update(scanTool(context.Background(), tools[0], Env{}, h.gen)())
	if text, _ := sumText(h.sums["ok"]); !strings.Contains(text, "≥") {
		t.Fatalf("统计不完整时应显示 ≥，实际 %q", text)
	}
	if _, _, partial := h.total(); !partial {
		t.Fatal("总量应标记为不完整")
	}
}
