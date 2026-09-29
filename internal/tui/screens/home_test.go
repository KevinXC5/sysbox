package screens

import (
	"errors"
	"testing"

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
		h.Update(scanTool(tl, Env{}, h.gen)())
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
