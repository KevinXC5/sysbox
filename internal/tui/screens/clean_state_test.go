package screens

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// listPage 返回一个已完成扫描、处于列表状态且默认勾选一项的清理页
func listPage(t *testing.T) *cleanPage {
	t.Helper()
	src := stubSource{items: []cleanup.Item{{Path: "/cache/a", Name: "a", Selectable: true, Selected: true, Sized: true}}}
	p := NewClean(src, Env{}).(*cleanPage)
	p.Update(cleanScannedMsg{src.items})
	p.Update(cleanScanReady{})
	if p.state != cleanList {
		t.Fatalf("应进入列表状态，实际 %d", p.state)
	}
	return p
}

// 确认弹窗默认焦点在“取消”，未提示的 y 键不能直接开始删除
func TestConfirmIgnoresHiddenYes(t *testing.T) {
	p := listPage(t)
	p.check()
	p.Update(cleanCheckedMsg{gen: p.checkGen})
	if p.state != cleanConfirm {
		t.Fatalf("应进入确认状态，实际 %d", p.state)
	}
	if cmd := p.onKey("y"); cmd != nil || p.state != cleanConfirm {
		t.Fatalf("y 不应开始删除，状态 %d", p.state)
	}
	p.onKey("enter")
	if p.state != cleanList {
		t.Fatal("焦点在取消时回车应回到列表")
	}
}

// 检查期间可以 esc 返回，之后迟到的检查结果不能再弹出确认框
func TestCheckingEscDropsLateResult(t *testing.T) {
	p := listPage(t)
	p.check()
	stale := p.checkGen
	p.onKey("esc")
	if p.state != cleanList {
		t.Fatalf("检查中 esc 应回到列表，实际 %d", p.state)
	}
	p.Update(cleanCheckedMsg{gen: stale})
	if p.state != cleanList {
		t.Fatal("过期的检查结果不应改变状态")
	}
}

// 列表静止时 spinner 不再续 tick，避免空闲重绘
func TestIdleSpinnerStops(t *testing.T) {
	p := listPage(t)
	if cmd := p.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatal("列表状态下 spinner 不应继续 tick")
	}
}

// blockingSource 扫描阻塞到调用方取消为止
type blockingSource struct {
	stubSource
	stopped chan struct{}
}

func (s blockingSource) ScanContext(ctx context.Context, _ func(string)) ([]cleanup.Item, error) {
	<-ctx.Done()
	close(s.stopped)
	return nil, ctx.Err()
}

// 扫描中返回首页必须取消后台扫描，不能留下阻塞的 goroutine
func TestLeavingScanCancels(t *testing.T) {
	src := blockingSource{stopped: make(chan struct{})}
	p := NewClean(src, Env{}).(*cleanPage)
	p.Init()
	if cmd := p.onKey("esc"); cmd == nil {
		t.Fatal("扫描中 esc 应返回首页")
	}
	select {
	case <-src.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("返回后扫描未被取消")
	}
}

// 删除后只重新测量改动过的根，Untouched 沿用清理前的大小
func TestAfterSizesSkipUntouched(t *testing.T) {
	src := stubSource{roots: []cleanup.Root{{Label: "配置", Path: "/does/not/exist", Untouched: true}}}
	p := NewClean(src, Env{}).(*cleanPage)
	p.before = []int64{1234}
	if got := p.measureRoots(context.Background(), true); got[0] != 1234 {
		t.Fatalf("Untouched 根应沿用清理前大小，实际 %d", got[0])
	}
}
