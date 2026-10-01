package screens

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/ui/search"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// 三个系统工具共用列表与独立滚动详情，查询逻辑由各自的数据源提供。
type systemRow struct {
	name, summary string
	lines         []string
	value         any
	target        string
}
type systemResult struct {
	rows []systemRow
	note string
}
type systemAction struct {
	key, label, note, preview, tool, navigate string
	mutates                                   bool
	run                                       func(context.Context) (string, error)
}
type systemSpec struct {
	name       string
	tabs       []string
	defaults   []string
	inputLabel string
	initialTab int
	timeout    time.Duration
	load       func(context.Context, int, string) (systemResult, error)
	actions    func(int, systemRow) []systemAction
	parent     func(string) string
}
type systemLoadedMsg struct {
	generation uint64
	result     systemResult
	err        error
}
type systemActedMsg struct {
	text   string
	err    error
	reload bool
}
type systemPage struct {
	env                                                        Env
	spec                                                       systemSpec
	ctx                                                        context.Context
	cancel, loadCancel                                         context.CancelFunc
	generation                                                 uint64
	tab, cursor, offset, rows, focus, detailOffset, detailRows int
	target, filter, inputMode, note, output                    string
	input                                                      textinput.Model
	loading, acting                                            bool
	result                                                     systemResult
	visible                                                    []systemRow
	confirm                                                    *systemAction
}

func newSystemPage(env Env, spec systemSpec) *systemPage {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.CharLimit = 2048
	tab := spec.initialTab
	target := ""
	if len(spec.defaults) > tab {
		target = spec.defaults[tab]
	}
	return &systemPage{env: env, spec: spec, ctx: ctx, cancel: cancel, input: input, tab: tab, target: target}
}
func (p *systemPage) Init() tea.Cmd { return p.reload() }
func (p *systemPage) Close() {
	p.cancel()
	if p.loadCancel != nil {
		p.loadCancel()
	}
}
func (p *systemPage) Busy() bool       { return p.acting }
func (p *systemPage) Typing() bool     { return p.inputMode != "" || p.confirm != nil }
func (p *systemPage) Crumbs() []string { return []string{"系统", p.spec.name} }
func (p *systemPage) Status() string {
	if p.loading {
		return "正在查询…"
	}
	if p.acting {
		return "正在执行…"
	}
	return fmt.Sprintf("%d 项", len(p.visible))
}
func (p *systemPage) Hints() []string {
	if p.confirm != nil {
		return []string{"y", "确认", "esc", "取消"}
	}
	if p.inputMode != "" {
		return []string{"enter", "确定", "esc", "取消"}
	}
	hints := []string{"tab", "切换类别", "←→", "切换面板", "↑↓", "选择 / 滚动", "/", "搜索", "r", "刷新"}
	if p.spec.inputLabel != "" {
		hints = append(hints, "g", p.spec.inputLabel)
	}
	if p.spec.parent != nil {
		hints = append(hints, "backspace", "上级目录")
	}
	if row, ok := p.current(); ok && p.spec.actions != nil {
		for _, a := range p.spec.actions(p.tab, row) {
			hints = append(hints, a.key, a.label)
		}
	}
	return append(hints, "esc", "返回")
}
func (p *systemPage) current() (systemRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return systemRow{}, false
	}
	return p.visible[p.cursor], true
}
func (p *systemPage) reload() tea.Cmd {
	if p.acting {
		return nil
	}
	if p.loadCancel != nil {
		p.loadCancel()
	}
	p.generation++
	generation, tab, target := p.generation, p.tab, p.target
	timeout := p.spec.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	p.loadCancel = cancel
	p.loading = true
	p.output = ""
	p.note = ""
	p.detailOffset = 0
	return func() tea.Msg {
		defer cancel()
		result, err := p.spec.load(ctx, tab, target)
		return systemLoadedMsg{generation, result, err}
	}
}
func (p *systemPage) rebuild() {
	type match struct {
		row   systemRow
		score int
	}
	matches := []match{}
	for _, row := range p.result.rows {
		fields := []string{resourceText(row.name), resourceText(row.summary)}
		for _, line := range row.lines {
			fields = append(fields, resourceText(line))
		}
		if score, ok := search.Score(p.filter, fields...); ok {
			matches = append(matches, match{row, score})
		}
	}
	// 完整名称和连续命中排在模糊匹配前面，保留相同分数条目的原有顺序。
	if p.filter != "" {
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	}
	p.visible = nil
	for _, match := range matches {
		p.visible = append(p.visible, match.row)
	}
	p.cursor = min(p.cursor, max(0, len(p.visible)-1))
	p.offset = 0
	p.detailOffset = 0
}

func (p *systemPage) execute(action systemAction) tea.Cmd {
	if action.navigate != "" {
		p.target = action.navigate
		p.tab = 0
		p.cursor = 0
		p.filter = ""
		return p.reload()
	}
	if action.tool != "" {
		return Open(action.tool)
	}
	if action.run == nil {
		return nil
	}
	if action.mutates && p.env.DryRun {
		p.note = "演练模式：将执行 " + action.preview
		return nil
	}
	p.acting = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
		defer cancel()
		text, err := action.run(ctx)
		return systemActedMsg{text, err, action.mutates}
	}
}
func (p *systemPage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case systemLoadedMsg:
		if msg.generation != p.generation {
			return nil
		}
		p.loading = false
		if msg.err != nil {
			p.note = "查询失败：" + msg.err.Error()
			p.result = systemResult{}
			p.visible = nil
			return nil
		}
		p.result = msg.result
		p.note = msg.result.note
		p.rebuild()
	case systemActedMsg:
		p.acting = false
		if msg.err != nil {
			p.note = msg.err.Error()
			return nil
		}
		if msg.reload {
			cmd := p.reload()
			p.note = resourceText(msg.text)
			return cmd
		}
		p.output = resourceText(msg.text)
		if strings.TrimSpace(p.output) == "" {
			p.output = "暂无日志或详情"
		}
		p.focus = 1
		p.detailOffset = 0
	case tea.KeyMsg:
		key := msg.String()
		if p.acting {
			return nil
		}
		if p.confirm != nil {
			if key == "esc" {
				p.confirm = nil
				return nil
			}
			if key == "y" {
				action := *p.confirm
				p.confirm = nil
				return p.execute(action)
			}
			return nil
		}
		if p.inputMode != "" {
			switch key {
			case "esc":
				p.inputMode = ""
				p.input.Blur()
				return nil
			case "enter":
				value := strings.TrimSpace(p.input.Value())
				mode := p.inputMode
				p.inputMode = ""
				p.input.Blur()
				if mode == "filter" {
					p.filter = value
					p.cursor = 0
					p.rebuild()
					return nil
				}
				if value == "" {
					p.note = "请输入" + p.spec.inputLabel
					return nil
				}
				p.target = value
				p.cursor = 0
				if p.spec.parent != nil {
					p.tab = 0
				}
				return p.reload()
			}
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return cmd
		}
		switch key {
		case "esc", "q":
			if p.output != "" {
				p.output = ""
				p.detailOffset = 0
				return nil
			}
			if p.filter != "" {
				p.filter = ""
				p.rebuild()
				return nil
			}
			return Back
		case "tab", "shift+tab":
			old := p.tab
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			p.tab = (p.tab + delta + len(p.spec.tabs)) % len(p.spec.tabs)
			if len(p.spec.defaults) > p.tab && len(p.spec.defaults) > old && p.target == p.spec.defaults[old] {
				p.target = p.spec.defaults[p.tab]
			}
			p.cursor = 0
			p.filter = ""
			return p.reload()
		case "left":
			p.focus = 0
		case "right":
			p.focus = 1
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			if key == "pgup" {
				delta = -max(1, p.rows)
			}
			if key == "pgdown" {
				delta = max(1, p.rows)
			}
			if p.focus == 0 {
				if key == "home" {
					p.cursor = 0
				} else if key == "end" {
					p.cursor = max(0, len(p.visible)-1)
				} else {
					p.cursor = max(0, min(len(p.visible)-1, p.cursor+delta))
				}
				p.output = ""
				p.detailOffset = 0
			} else {
				p.detailOffset = max(0, min(max(0, p.detailRows-p.rows), p.detailOffset+delta))
				if key == "home" {
					p.detailOffset = 0
				}
				if key == "end" {
					p.detailOffset = max(0, p.detailRows-p.rows)
				}
			}
		case "/", "g":
			if key == "g" && p.spec.inputLabel == "" {
				return nil
			}
			p.inputMode = "filter"
			value := p.filter
			p.input.Prompt = "/ "
			if key == "g" {
				p.inputMode = "target"
				value = p.target
				p.input.Prompt = p.spec.inputLabel + "："
			}
			p.input.SetValue(value)
			p.input.CursorEnd()
			return p.input.Focus()
		case "r":
			return p.reload()
		case "backspace":
			if p.spec.parent != nil {
				p.target = p.spec.parent(p.target)
				p.tab = 0
				p.cursor = 0
				p.filter = ""
				return p.reload()
			}
		case "enter":
			if p.loading {
				return nil
			}
			if row, ok := p.current(); ok {
				if row.target != "" {
					return p.execute(systemAction{navigate: row.target})
				}
				p.focus = 1
			}
		default:
			// 加载期间保留旧列表供浏览，但不能对尚未更新的目标执行操作。
			if p.loading {
				return nil
			}
			if row, ok := p.current(); ok && p.spec.actions != nil {
				for _, action := range p.spec.actions(p.tab, row) {
					if key == action.key {
						if action.mutates {
							copy := action
							p.confirm = &copy
							return nil
						}
						return p.execute(action)
					}
				}
			}
		}
	}
	return nil
}
func (p *systemPage) Body(w, h int) string {
	iw := max(1, w-4)
	tabs := []string{}
	for i, name := range p.spec.tabs {
		style := theme.MutedStyle
		if i == p.tab {
			style = theme.Fg(theme.Accent).Bold(true)
		}
		tabs = append(tabs, style.Render(" "+name+" "))
	}
	top := []string{strings.Join(tabs, "  ")}
	if p.spec.inputLabel != "" {
		top = append(top, theme.SubtleStyle.Render(theme.Truncate(p.spec.inputLabel+"："+resourceText(p.target), iw)))
	}
	if p.inputMode != "" {
		p.input.Width = max(8, iw-20)
		top = append(top, p.input.View())
	} else {
		top = append(top, theme.HintsFit(iw, p.Hints()...))
	}
	if p.note != "" {
		top = append(top, theme.Fg(theme.Amber).Render(theme.Truncate(resourceText(p.note), iw)))
	}
	top = append(top, "")
	height := max(5, h-len(top))
	lw := iw * 40 / 100
	rw := iw - lw - 2
	if iw < 76 {
		lw = iw
		rw = 0
	}
	p.rows = max(1, height-6)
	p.offset = widget.Scroll(p.cursor, p.offset, len(p.visible), p.rows)
	left := []string{theme.BoldStyle.Render(p.spec.tabs[p.tab] + " · " + fmt.Sprint(len(p.visible))), theme.Rule(max(1, lw-6))}
	for i := p.offset; i < min(len(p.visible), p.offset+p.rows); i++ {
		row := p.visible[i]
		prefix := "  "
		style := theme.TextStyle
		if i == p.cursor {
			prefix = "▌ "
			style = theme.Fg(theme.Accent).Bold(true)
		}
		nameW := max(1, lw-8-min(20, lw/3))
		summaryW := max(1, lw-8-nameW)
		left = append(left, prefix+widget.Cell(resourceText(row.name), nameW, style, false)+widget.Cell(resourceText(row.summary), summaryW, theme.SubtleStyle, true))
	}
	if len(p.visible) == 0 {
		label := "暂无条目"
		if p.loading {
			label = "正在查询，请稍候…"
		}
		left = append(left, "", theme.MutedStyle.Render(label))
	}
	border := theme.Faint
	if p.focus == 0 {
		border = theme.Accent
	}
	body := widget.Panel(lw, height, border, strings.Join(left, "\n"))
	if rw > 0 || p.focus == 1 {
		narrow := rw == 0
		if narrow {
			rw = iw
		}
		lines := []string{"请选择条目"}
		title := "详情"
		if p.output != "" {
			lines = strings.Split(p.output, "\n")
			title = "执行结果"
		} else if row, ok := p.current(); ok {
			lines = row.lines
			title = resourceText(row.name)
		}
		wrapped := strings.Split(ansi.Hardwrap(resourceText(strings.Join(lines, "\n")), max(1, rw-6), true), "\n")
		p.detailRows = len(wrapped)
		p.detailOffset = min(p.detailOffset, max(0, len(wrapped)-p.rows))
		right := []string{theme.BoldStyle.Render(theme.Truncate(title, max(1, rw-6))), theme.Rule(max(1, rw-6))}
		right = append(right, wrapped[p.detailOffset:min(len(wrapped), p.detailOffset+p.rows)]...)
		border = theme.Faint
		if p.focus == 1 {
			border = theme.Accent
		}
		panel := widget.Panel(rw, height, border, strings.Join(right, "\n"))
		if narrow {
			body = panel
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, "  ", panel)
		}
	}
	body = widget.Inset(strings.Join(top, "\n") + "\n" + body)
	if p.confirm != nil {
		dw := widget.DialogWidth(82, w)
		inner := max(1, widget.DialogInner(dw))
		action := p.confirm
		text := theme.BoldStyle.Render("确认"+action.label+"？") + "\n\n" + ansi.Hardwrap(resourceText(action.note+"\n\n"+action.preview), inner, true) + "\n\n" + theme.HintsFit(inner, "y", "确认", "esc", "取消")
		body = widget.Overlay(body, widget.Dialog(theme.Amber, dw, text), w, h)
	}
	return body
}
