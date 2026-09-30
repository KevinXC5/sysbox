package screens

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
)

// toolSum 首页上一个清理工具的扫描汇总
type toolSum struct {
	gen     int // 扫描批次，旧批次的结果到达时丢弃
	done    bool
	n       int
	size    int64
	partial bool // 有条目统计不完整，size 可能偏小
	failure string
	cancel  context.CancelFunc
}

// homeSumMsg 一个工具扫描完成
type homeSumMsg struct {
	id      string
	gen     int
	n       int
	size    int64
	partial bool
	err     error
}

// estimate 扫描一个清理工具，统计默认勾选条目的数量和大小，口径与进入工具后的“已选”一致
func estimate(ctx context.Context, src cleanup.Source) (sum homeSumMsg) {
	var items []cleanup.Item
	if cs, ok := src.(cleanup.ContextScanner); ok {
		items, sum.err = cs.ScanContext(ctx, func(string) {})
	} else {
		items, sum.err = src.Scan(func(string) {})
	}
	if sum.err != nil {
		return sum
	}
	var paths []string
	for _, it := range items {
		if !it.Selectable || !it.Selected {
			continue
		}
		sum.n++
		if it.Sized {
			sum.size += it.Size
			sum.partial = sum.partial || it.Partial
		} else {
			paths = append(paths, it.Path)
		}
	}
	var mu sync.Mutex
	fsx.MeasureAllContext(ctx, paths, 4, func(_ int, u fsx.Usage) {
		mu.Lock()
		sum.size += u.Bytes
		sum.partial = sum.partial || u.Partial()
		mu.Unlock()
	})
	return sum
}

// scanTool 在后台扫描一个工具；取消后丢弃结果，不回传消息
func scanTool(ctx context.Context, t Tool, env Env, gen int) tea.Cmd {
	return func() tea.Msg {
		var sum homeSumMsg
		if src, err := t.Source(env); err != nil {
			sum.err = err
		} else {
			sum = estimate(ctx, src)
		}
		if ctx.Err() != nil {
			return nil
		}
		sum.id, sum.gen = t.ID, gen
		return sum
	}
}
