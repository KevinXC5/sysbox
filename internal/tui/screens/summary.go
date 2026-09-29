package screens

import (
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
	failure string
}

// homeSumMsg 一个工具扫描完成
type homeSumMsg struct {
	id   string
	gen  int
	n    int
	size int64
	err  error
}

// estimate 扫描一个清理工具，统计默认勾选条目的数量和大小，口径与进入工具后的“已选”一致
func estimate(src cleanup.Source) (n int, size int64, err error) {
	items, err := src.Scan(func(string) {})
	if err != nil {
		return 0, 0, err
	}
	var paths []string
	for _, it := range items {
		if !it.Selectable || !it.Selected {
			continue
		}
		n++
		if it.Sized {
			size += it.Size
		} else {
			paths = append(paths, it.Path)
		}
	}
	var mu sync.Mutex
	fsx.MeasureAll(paths, 4, func(_ int, s int64) {
		mu.Lock()
		size += s
		mu.Unlock()
	})
	return n, size, nil
}

// scanTool 在后台扫描一个工具
func scanTool(t Tool, env Env, gen int) tea.Cmd {
	return func() tea.Msg {
		src, err := t.Source(env)
		if err != nil {
			return homeSumMsg{id: t.ID, gen: gen, err: err}
		}
		n, size, err := estimate(src)
		return homeSumMsg{id: t.ID, gen: gen, n: n, size: size, err: err}
	}
}
