package screens

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/tools/network"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
	tea "github.com/charmbracelet/bubbletea"
)

type networkCheck struct {
	generation uint64
	cancel     context.CancelFunc
}
type networkCheckedMsg struct {
	target     string
	generation uint64
	report     network.Report
}
type networkProxyMsg struct{ proxy network.ProxySettings }
type networkComparedMsg struct {
	generation uint64
	result     systemResult
	err        error
}

// 每个目标单独交付结果，慢目标不会阻塞其他条目的查看与重测。
type networkPage struct {
	*systemPage
	state             *networkState
	checks            map[string]networkCheck
	sequence          uint64
	limit             chan struct{}
	checker           func(context.Context, string, network.Via) network.Report
	detail            bool
	compare           *systemPage
	compareGeneration uint64
	compareCancel     context.CancelFunc
}

func (p *networkPage) Init() tea.Cmd {
	return tea.Batch(p.startTargets(false), func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		proxy, _ := network.SystemProxy(ctx)
		return networkProxyMsg{proxy}
	})
}
func (p *networkPage) Close() {
	if p.compare != nil {
		p.compare.Close()
	}
	p.systemPage.Close()
}
func (p *networkPage) syncTargets() {
	result := p.state.rows()
	p.cache["0\x00"] = result
	if p.tab == 0 {
		p.result = result
		p.rebuild(true)
	}
}
func (p *networkPage) startTarget(target string) tea.Cmd {
	if check, ok := p.checks[target]; ok {
		check.cancel()
	}
	p.sequence++
	generation := p.sequence
	ctx, cancel := context.WithCancel(p.ctx)
	p.checks[target] = networkCheck{generation, cancel}
	p.state.mu.Lock()
	delete(p.state.reports, target)
	p.state.pending[target] = true
	p.state.mu.Unlock()
	p.syncTargets()
	checker := p.checker
	if checker == nil {
		checker = network.Check
	}
	return func() tea.Msg {
		defer cancel()
		select {
		case p.limit <- struct{}{}:
			defer func() { <-p.limit }()
		case <-ctx.Done():
			return networkCheckedMsg{target, generation, network.Report{Target: target, Failed: "检测", Err: ctx.Err()}}
		}
		checkCtx, checkCancel := context.WithTimeout(ctx, 12*time.Second)
		defer checkCancel()
		return networkCheckedMsg{target, generation, checker(checkCtx, target, network.Via{Mode: network.RouteEnv})}
	}
}
func (p *networkPage) startTargets(force bool) tea.Cmd {
	p.state.mu.Lock()
	targets := p.state.targets()
	p.state.mu.Unlock()
	var cmds []tea.Cmd
	for _, target := range targets {
		p.state.mu.Lock()
		_, done := p.state.reports[target.target]
		p.state.mu.Unlock()
		_, pending := p.checks[target.target]
		if force || !done && !pending {
			cmds = append(cmds, p.startTarget(target.target))
		}
	}
	p.syncTargets()
	return tea.Batch(cmds...)
}
func (p *networkPage) Hints() []string {
	if p.compare != nil && p.compare.Typing() {
		return p.compare.Hints()
	}
	if p.compare != nil && p.compare.focus == 1 {
		return []string{"↑↓", "滚动", "PgUp/PgDn", "翻页", "esc", "返回对比"}
	}
	if p.compare != nil {
		return []string{"↑↓", "选择", "enter", "链路详情", "c", "复制代理命令", "esc", "关闭对比"}
	}
	if p.detail {
		return []string{"↑↓", "滚动", "PgUp/PgDn", "翻页", "esc", "返回表格"}
	}
	if p.Typing() {
		return p.systemPage.Hints()
	}
	if p.tab == 0 {
		return []string{"tab", "类别", "↑↓", "选择", "enter", "详情", "r", "重测当前", "R", "重测全部", "p", "对比线路", "/", "搜索", "esc", p.backLabel()}
	}
	return []string{"tab", "类别", "↑↓", "选择", "enter", "详情", "r", "刷新", "/", "搜索", "esc", "返回"}
}
func (p *networkPage) compareTarget(target string) tea.Cmd {
	if p.compareCancel != nil {
		p.compareCancel()
	}
	p.compareGeneration++
	generation := p.compareGeneration
	ctx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
	p.compareCancel = cancel
	if p.compare != nil {
		p.compare.Close()
	}
	spec := p.spec
	spec.tabs = []string{"线路对比"}
	spec.initialTab = 0
	spec.columns = func(int) []systemColumn {
		return []systemColumn{{title: "线路", width: 18}, {title: "结果", width: 22}, {title: "耗时", width: 9, right: true}, {title: "代理 / 说明"}}
	}
	spec.actions = func(int, systemRow) []systemAction {
		return []systemAction{{key: "enter", label: "链路详情"}, {key: "c", label: "复制代理命令"}}
	}
	spec.input = nil
	p.compare = newSystemPage(p.env, spec)
	p.compare.result.info = "正在对比：" + target
	p.compare.loading = true
	return func() tea.Msg {
		defer cancel()
		result, err := p.state.loadProxy(ctx, target)
		return networkComparedMsg{generation, result, err}
	}
}
func (p *networkPage) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case networkCheckedMsg:
		check, ok := p.checks[m.target]
		if !ok || check.generation != m.generation {
			return nil
		}
		delete(p.checks, m.target)
		p.state.mu.Lock()
		delete(p.state.pending, m.target)
		p.state.reports[m.target] = m.report
		p.state.checked[m.target] = time.Now()
		p.state.mu.Unlock()
		p.syncTargets()
		return nil
	case networkProxyMsg:
		p.state.mu.Lock()
		p.state.proxy = m.proxy
		p.state.mu.Unlock()
		p.syncTargets()
		return nil
	case networkComparedMsg:
		if p.compare == nil || m.generation != p.compareGeneration {
			return nil
		}
		q := p.compare
		q.loading = false
		q.result = systemResult{info: strings.Split(m.result.info, " · ")[0], warn: m.result.warn}
		if m.err != nil {
			q.result.warn = "线路对比失败：" + m.err.Error()
		}
		for _, row := range m.result.rows {
			if row.key == "conclusion" {
				for _, f := range row.fields {
					if f.Label == "状态" {
						q.result.info += " · " + f.Value
					}
				}
				continue
			}
			if !strings.HasPrefix(row.key, "check-") {
				continue
			}
			if report, ok := row.value.(network.Report); ok {
				row.cells, row.tone = reportCells(report)
			} else {
				row.cells = []string{row.cells[0], "", row.cells[1]}
			}
			row.name = strings.TrimPrefix(row.name, "检测：")
			q.result.rows = append(q.result.rows, row)
		}
		q.rebuild(false)
		return nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		key := k.String()
		if p.compare != nil {
			q := p.compare
			if q.acting {
				return nil
			}
			if q.Typing() {
				return q.Update(msg)
			}
			if key == "esc" || key == "q" {
				if q.focus == 1 {
					q.focus = 0
					return nil
				}
				if p.compareCancel != nil {
					p.compareCancel()
				}
				q.Close()
				p.compare = nil
				p.compareGeneration++
				return nil
			}
			if key == "enter" {
				if _, ok := q.current(); !ok {
					return nil
				}
				q.focus = 1
				return nil
			}
			if key == "c" { // 复制只改变剪贴板，不宣称当前进程已应用代理。
				p.state.mu.Lock()
				command := network.ExportCommand(p.state.proxy, runtime.GOOS == "windows")
				p.state.mu.Unlock()
				if command == "" {
					q.notice = "没有可复制的系统代理地址"
					return nil
				}
				return q.execute(systemAction{label: "复制代理命令", quiet: true, run: func(ctx context.Context) (string, error) {
					return "已复制；在执行命令的终端重新启动 sysbox 后生效", copyText(ctx, command)
				}})
			}
			if key == "left" || key == "right" || key == "tab" || key == "r" || key == "g" {
				return nil
			}
			return q.Update(msg)
		}
		if p.detail {
			switch key {
			case "esc", "q", "enter":
				p.detail = false
				p.focus = 0
				return nil
			case "up", "down", "pgup", "pgdown", "home", "end":
				return p.systemPage.Update(msg)
			}
			return nil
		}
		if !p.Typing() && !p.acting {
			switch key {
			case "esc":
				if p.tab == 0 && p.filter == "" && len(p.checks) > 0 {
					p.state.mu.Lock()
					for target, check := range p.checks {
						check.cancel()
						delete(p.state.pending, target)
						p.state.reports[target] = network.Report{Target: target, Address: target, Failed: "检测", Err: context.Canceled, Steps: []network.Step{{Name: "检测", Detail: "已取消"}}}
						p.state.checked[target] = time.Now()
					}
					p.state.mu.Unlock()
					clear(p.checks)
					p.syncTargets()
					return nil
				}
			case "left", "right":
				return nil
			case "enter":
				if _, ok := p.current(); ok {
					p.detail = true
					p.focus = 1
					p.detailOffset = 0
				}
				return nil
			case "p":
				if p.tab == 0 {
					if row, ok := p.current(); ok {
						return p.compareTarget(row.key)
					}
				}
				return nil
			case "r":
				if p.tab == 0 {
					if row, ok := p.current(); ok {
						return p.startTarget(row.key)
					}
					return nil
				}
			case "R":
				if p.tab == 0 {
					return p.startTargets(true)
				}
				return nil
			case "x":
				if p.tab == 0 {
					if row, ok := p.current(); ok && p.state.isCustom(row.key) {
						if check, ok := p.checks[row.key]; ok {
							check.cancel()
							delete(p.checks, row.key)
						}
						p.state.remove(row.key)
						p.syncTargets()
					}
					return nil
				}
			}
		}
	}
	if m, ok := msg.(systemActedMsg); ok && p.compare != nil {
		return p.compare.Update(m)
	}
	adding := false
	if k, ok := msg.(tea.KeyMsg); ok {
		adding = p.tab == 0 && p.inputMode == "target" && k.String() == "enter" && strings.TrimSpace(p.input.Value()) != ""
	}
	cmd := p.systemPage.Update(msg)
	if !p.detail {
		p.focus = 0
	}
	if adding {
		return tea.Batch(cmd, p.startTargets(false))
	}
	return cmd
}
func (p *networkPage) Body(w, h int) string {
	if p.compare != nil {
		q := p.compare
		if q.focus == 1 {
			return widget.Inset(q.detailPane(max(1, w-4), max(1, h-2)))
		}
		return q.Body(w, h)
	}
	if p.detail {
		return widget.Inset(p.detailPane(max(1, w-4), max(1, h-2)))
	}
	return p.systemPage.Body(w, h)
}

func (p *networkPage) backLabel() string {
	if len(p.checks) > 0 && p.filter == "" {
		return "取消检测"
	}
	if p.filter != "" {
		return "清除搜索"
	}
	return "返回"
}

func (p *networkPage) Typing() bool {
	if p.compare != nil {
		return p.compare.Typing()
	}
	return p.systemPage.Typing()
}
func (p *networkPage) Busy() bool {
	return p.systemPage.Busy() || p.compare != nil && p.compare.Busy()
}
