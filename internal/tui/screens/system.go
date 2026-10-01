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

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/ui/search"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// 系统工具共用的快捷管理页：左侧多列表格，右侧分组详情；类别切换使用缓存，视图与搜索在本地完成。
type systemColumn struct {
	title string
	width int // 0 表示平分剩余宽度
	right bool
	bar   bool // 按 row.ratio 绘制占比条
}
type systemRow struct {
	name      string
	cells     []string // 依次对应第二列起的各列
	tone      lipgloss.TerminalColor
	ratio     float64 // 小于 0 时不绘制占比条
	fields    []containers.Field
	value     any
	target    string // 按 enter 进入的目标
	targetTab int
	key       string // 刷新后保持光标的稳定标识
}

func (r systemRow) id() string {
	if r.key != "" {
		return r.key
	}
	return r.name
}

type systemResult struct {
	rows       []systemRow
	info, warn string
}
type systemAction struct {
	key, label, note, preview, tool string
	mutates, reload, quiet          bool
	run                             func(context.Context) (string, error)
}
type systemSpec struct {
	name       string
	tabs       []string
	initialTab int
	target     string
	timeout    time.Duration // 0 表示不限时，可按 esc 取消
	columns    func(tab int) []systemColumn
	views      []string
	view       func(view int, row systemRow) bool
	input      func(tab int) string                      // 输入框标签，空表示该类别不支持
	submit     func(tab int, value string) (int, string) // 输入完成后要切换到的类别与目标
	parent     func(tab int, target string) (string, bool)
	load       func(ctx context.Context, tab int, target string, force bool) (systemResult, error)
	live       func(tab int) (systemResult, bool) // 加载期间定时读取进度或部分结果
	actions    func(tab int, row systemRow) []systemAction
}
type systemLoadedMsg struct {
	generation uint64
	key        string
	result     systemResult
	err        error
}
type systemLiveMsg struct{ generation uint64 }
type systemActedMsg struct {
	label, text            string
	err                    error
	reload, output, change bool
}
type systemPage struct {
	env                                                    Env
	spec                                                   systemSpec
	ctx                                                    context.Context
	cancel, loadCancel                                     context.CancelFunc
	generation                                             uint64
	tab, view, cursor, offset, rows, focus                 int
	detailOffset, detailRows, detailView                   int
	target, filter, inputMode, notice, output, outputTitle string
	noticeErr, loading, acting                             bool
	input                                                  textinput.Model
	result                                                 systemResult
	visible                                                []systemRow
	confirm                                                *systemAction
	cache                                                  map[string]systemResult
	pending                                                tea.Cmd
}

func newSystemPage(env Env, spec systemSpec) *systemPage {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.CharLimit = 2048
	return &systemPage{env: env, spec: spec, ctx: ctx, cancel: cancel, input: input, tab: spec.initialTab, target: spec.target, cache: map[string]systemResult{}}
}
func (p *systemPage) Init() tea.Cmd { return p.reload(false) }
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
	switch {
	case p.acting:
		return "正在执行…"
	case p.loading:
		return "正在读取…"
	}
	return fmt.Sprintf("%d / %d 项", len(p.visible), len(p.result.rows))
}

// Hints 底栏只放导航按键，当前条目的操作显示在页面顶部
func (p *systemPage) Hints() []string {
	if p.confirm != nil {
		return []string{"y", "确认", "esc", "取消"}
	}
	if p.inputMode != "" {
		return []string{"enter", "确定", "esc", "取消"}
	}
	back := "返回"
	if p.loading && p.spec.timeout == 0 {
		back = "取消读取"
	} else if p.output != "" {
		back = "关闭输出"
	} else if p.filter != "" {
		back = "清除搜索"
	}
	return []string{"tab", "类别", "←→", "面板", "↑↓", "选择 / 滚动", "/", "搜索", "r", "刷新", "esc", back}
}
func (p *systemPage) current() (systemRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return systemRow{}, false
	}
	return p.visible[p.cursor], true
}
func (p *systemPage) cacheKey() string { return fmt.Sprintf("%d\x00%s", p.tab, p.target) }

func (p *systemPage) reload(force bool) tea.Cmd {
	if p.acting {
		return nil
	}
	if p.loadCancel != nil {
		p.loadCancel()
	}
	p.generation++
	generation, tab, target, key := p.generation, p.tab, p.target, p.cacheKey()
	ctx, cancel := context.WithCancel(p.ctx)
	if p.spec.timeout > 0 {
		ctx, cancel = context.WithTimeout(p.ctx, p.spec.timeout)
	}
	p.loadCancel = cancel
	p.loading = true
	p.output = ""
	load := func() tea.Msg {
		defer cancel()
		result, err := p.spec.load(ctx, tab, target, force)
		return systemLoadedMsg{generation, key, result, err}
	}
	if p.spec.live != nil {
		return tea.Batch(load, p.liveTick(generation))
	}
	return load
}
func (p *systemPage) liveTick(generation uint64) tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return systemLiveMsg{generation} })
}

// show 切换类别或目标：有缓存时直接展示，否则重新读取
func (p *systemPage) show() tea.Cmd {
	p.cursor, p.offset, p.detailOffset, p.output = 0, 0, 0, ""
	p.filter = ""
	if cached, ok := p.cache[p.cacheKey()]; ok {
		if p.loadCancel != nil {
			p.loadCancel()
		}
		p.generation++
		p.loading = false
		p.result = cached
		p.rebuild(false)
		return nil
	}
	p.result = systemResult{}
	p.visible = nil
	return p.reload(false)
}

func (p *systemPage) rebuild(keep bool) {
	previous, had := p.current()
	type match struct {
		row   systemRow
		score int
	}
	var matches []match
	for _, row := range p.result.rows {
		if p.spec.view != nil && !p.spec.view(p.view, row) {
			continue
		}
		fields := []string{resourceText(row.name)}
		for _, cell := range row.cells {
			fields = append(fields, resourceText(cell))
		}
		for _, f := range row.fields {
			fields = append(fields, resourceText(f.Value))
		}
		if score, ok := search.Score(p.filter, fields...); ok {
			matches = append(matches, match{row, score})
		}
	}
	// 完整名称和连续命中排在模糊匹配前面，保留相同分数条目的原有顺序
	if p.filter != "" {
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	}
	p.visible = p.visible[:0]
	for _, m := range matches {
		p.visible = append(p.visible, m.row)
	}
	cursor := 0
	if keep && had {
		cursor = min(p.cursor, max(0, len(p.visible)-1))
		for i, row := range p.visible {
			if row.id() == previous.id() {
				cursor = i
				break
			}
		}
	}
	if cursor != p.cursor {
		p.detailOffset = 0
	}
	p.cursor = cursor
}

func (p *systemPage) execute(action systemAction) tea.Cmd {
	if action.tool != "" {
		return Open(action.tool)
	}
	if action.run == nil {
		return nil
	}
	if action.mutates && p.env.DryRun {
		p.notice, p.noticeErr = "演练模式：将执行 "+action.preview, false
		return nil
	}
	p.acting = true
	p.notice = ""
	return func() tea.Msg {
		// 管理员授权需要等待用户输入密码，给足时间
		ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
		defer cancel()
		text, err := action.run(ctx)
		return systemActedMsg{label: action.label, text: text, err: err, reload: action.mutates || action.reload, output: !action.mutates && !action.reload && !action.quiet, change: action.mutates}
	}
}

func (p *systemPage) navigate(target string, tab int) tea.Cmd {
	p.target, p.tab = target, tab
	return p.show()
}

func (p *systemPage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case systemLoadedMsg:
		if msg.generation != p.generation {
			return nil
		}
		p.loading = false
		if msg.err != nil && len(msg.result.rows) == 0 {
			p.result = systemResult{warn: "读取失败：" + msg.err.Error()}
			p.rebuild(false)
			return nil
		}
		p.result = msg.result
		if msg.err == nil {
			p.cache[msg.key] = msg.result
		}
		p.rebuild(true)
	case systemLiveMsg:
		if msg.generation != p.generation || !p.loading {
			return nil
		}
		if result, ok := p.spec.live(p.tab); ok {
			p.result = result
			p.rebuild(true)
		}
		return p.liveTick(msg.generation)
	case systemActedMsg:
		p.acting = false
		if msg.err != nil {
			p.notice, p.noticeErr = msg.label+"失败："+msg.err.Error(), true
			return nil
		}
		if msg.output {
			p.output = resourceText(msg.text)
			if strings.TrimSpace(p.output) == "" {
				p.output = "暂无内容"
			}
			p.outputTitle, p.focus, p.detailOffset = msg.label, 1, 0
			return nil
		}
		p.notice, p.noticeErr = strings.TrimSpace(resourceText(msg.text)), false
		if msg.change {
			// 修改会影响其他类别的数据，清空缓存
			p.cache = map[string]systemResult{}
		}
		if msg.reload {
			return p.reload(false)
		}
	case tea.KeyMsg:
		return p.key(msg)
	}
	return nil
}

func (p *systemPage) key(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if p.acting {
		return nil
	}
	if p.confirm != nil {
		switch key {
		case "esc", "n":
			p.confirm = nil
		case "y":
			action := *p.confirm
			p.confirm = nil
			return p.execute(action)
		}
		return nil
	}
	if p.inputMode != "" {
		switch key {
		case "esc":
			if p.inputMode == "filter" {
				p.filter = ""
				p.rebuild(true)
			}
			p.inputMode = ""
			p.input.Blur()
			return nil
		case "enter":
			value := strings.TrimSpace(p.input.Value())
			mode := p.inputMode
			p.inputMode = ""
			p.input.Blur()
			if mode == "filter" || value == "" {
				return nil
			}
			tab, target := p.tab, value
			if p.spec.submit != nil {
				tab, target = p.spec.submit(p.tab, value)
			}
			// 主动输入的目标总是重新读取
			p.tab, p.target = tab, target
			delete(p.cache, p.cacheKey())
			return p.show()
		case "up", "down":
			p.move(map[string]int{"up": -1, "down": 1}[key], key)
			return nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		if p.inputMode == "filter" {
			p.filter = p.input.Value()
			p.rebuild(true)
		}
		return cmd
	}
	switch key {
	case "esc", "q":
		switch {
		case p.loading && p.spec.timeout == 0 && p.loadCancel != nil:
			p.loadCancel()
		case p.output != "":
			p.output, p.detailOffset = "", 0
		case p.filter != "":
			p.filter = ""
			p.rebuild(true)
		default:
			return Back
		}
		return nil
	case "tab", "shift+tab":
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		p.tab = (p.tab + delta + len(p.spec.tabs)) % len(p.spec.tabs)
		return p.show()
	case "left":
		p.focus = 0
	case "right":
		p.focus = 1
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		page := p.rows
		if p.focus == 1 {
			page = p.detailView
		}
		p.move(map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -max(1, page), "pgdown": max(1, page)}[key], key)
	case "/":
		p.inputMode = "filter"
		p.input.Prompt = "/ "
		p.input.SetValue(p.filter)
		p.input.CursorEnd()
		return p.input.Focus()
	case "g":
		if p.spec.input == nil || p.spec.input(p.tab) == "" {
			return nil
		}
		p.inputMode = "target"
		p.input.Prompt = p.spec.input(p.tab) + "："
		p.input.SetValue(p.target)
		p.input.CursorEnd()
		return p.input.Focus()
	case "f":
		if len(p.spec.views) > 0 {
			p.view = (p.view + 1) % len(p.spec.views)
			p.rebuild(true)
		}
	case "r":
		delete(p.cache, p.cacheKey())
		return p.reload(true)
	case "backspace":
		if p.spec.parent != nil {
			if parent, ok := p.spec.parent(p.tab, p.target); ok {
				return p.navigate(parent, p.tab)
			}
		}
	case "enter":
		if row, ok := p.current(); ok && !p.loading {
			if row.target != "" {
				return p.navigate(row.target, row.targetTab)
			}
			if p.runAction("enter") {
				return p.pending
			}
		}
		p.focus = 1
	default:
		if p.runAction(key) {
			return p.pending
		}
	}
	return nil
}

func (p *systemPage) move(delta int, key string) {
	if p.focus == 0 || p.inputMode != "" {
		switch key {
		case "home":
			p.cursor = 0
		case "end":
			p.cursor = max(0, len(p.visible)-1)
		default:
			p.cursor = max(0, min(len(p.visible)-1, p.cursor+delta))
		}
		p.output, p.detailOffset = "", 0
		return
	}
	limit := max(0, p.detailRows-p.detailView)
	switch key {
	case "home":
		p.detailOffset = 0
	case "end":
		p.detailOffset = limit
	default:
		p.detailOffset = max(0, min(limit, p.detailOffset+delta))
	}
}

// runAction 查找当前条目的快捷操作；修改类操作先进入确认
func (p *systemPage) runAction(key string) bool {
	p.pending = nil
	// 加载期间保留旧列表供浏览，但不能对尚未更新的目标执行操作
	if p.loading || p.spec.actions == nil {
		return false
	}
	row, ok := p.current()
	if !ok {
		return false
	}
	for _, action := range p.spec.actions(p.tab, row) {
		if action.key != key {
			continue
		}
		if action.mutates {
			copy := action
			p.confirm = &copy
			return true
		}
		p.pending = p.execute(action)
		return true
	}
	return false
}

func (p *systemPage) Body(w, h int) string {
	iw := max(1, w-4)
	var top []string
	tabs := make([]string, 0, len(p.spec.tabs))
	for i, label := range p.spec.tabs {
		style := lipgloss.NewStyle().Foreground(theme.Muted).Background(theme.Surface).Padding(0, 2)
		if i == p.tab {
			style = style.Foreground(theme.OnColor).Background(theme.Accent).Bold(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, interleaveTabs(tabs)...) + "   " + theme.Key("Tab") + theme.MutedStyle.Render(" 切换类别")
	top = append(top, theme.Truncate(bar, iw))
	if info := p.result.info; info != "" {
		top = append(top, theme.Truncate(theme.SubtleStyle.Render(resourceText(info)), iw))
	}
	top = append(top, "")
	if p.inputMode == "target" {
		p.input.Width = max(8, iw-lipgloss.Width(p.input.Prompt)-2)
		p.input.PromptStyle = theme.Fg(theme.Accent)
		top = append(top, p.input.View())
	} else {
		top = append(top, p.quickHints(iw))
	}
	if p.notice != "" {
		color := theme.Green
		if p.noticeErr {
			color = theme.Rose
		}
		top = append(top, theme.Truncate(theme.Fg(color).Render(resourceText(strings.ReplaceAll(p.notice, "\n", " · "))), iw))
	}
	if p.result.warn != "" {
		top = append(top, theme.Truncate(theme.Fg(theme.Amber).Render(resourceText(p.result.warn)), iw))
	}
	if p.env.DryRun {
		top = append(top, theme.Fg(theme.Amber).Render("演练模式 · 修改不会执行"))
	}
	head := strings.Join(top, "\n")
	room := max(8, h-lipgloss.Height(head)-1)
	gap := 2
	lw := (iw - gap) * 2 / 5
	rw := iw - gap - lw
	var body string
	switch {
	case iw < 76 && p.focus == 1:
		body = p.detailPane(iw, room)
	case iw < 76:
		body = p.listPane(iw, room)
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top, p.listPane(lw, room), strings.Repeat(" ", gap), p.detailPane(rw, room))
	}
	page := widget.Inset(head + "\n" + body)
	if p.confirm != nil {
		dw := widget.DialogWidth(82, w)
		inner := max(1, widget.DialogInner(dw))
		action := p.confirm
		text := theme.BoldStyle.Render("确认" + action.label + "？")
		if row, ok := p.current(); ok {
			text += "\n" + theme.SubtleStyle.Render(theme.Truncate(resourceText(row.name), inner))
		}
		if action.note != "" {
			text += "\n\n" + ansi.Hardwrap(resourceText(action.note), inner, true)
		}
		if action.preview != "" {
			text += "\n\n" + theme.MutedStyle.Render(ansi.Hardwrap(resourceText(action.preview), inner, true))
		}
		text += "\n\n" + theme.HintsFit(inner, "y", "确认", "esc", "取消")
		page = widget.Overlay(page, widget.Dialog(theme.Amber, dw, text), w, h)
	}
	return page
}

// quickHints 顶部只列出当前条目可用的操作，最多两行
func (p *systemPage) quickHints(w int) string {
	var pairs []string
	if row, ok := p.current(); ok {
		if row.target != "" {
			pairs = append(pairs, "enter", "进入")
		}
		if p.spec.actions != nil {
			for _, a := range p.spec.actions(p.tab, row) {
				pairs = append(pairs, a.key, a.label)
			}
		}
	}
	if p.spec.input != nil && p.spec.input(p.tab) != "" {
		pairs = append(pairs, "g", p.spec.input(p.tab))
	}
	if len(p.spec.views) > 0 {
		pairs = append(pairs, "f", "切换视图")
	}
	if p.spec.parent != nil {
		if _, ok := p.spec.parent(p.tab, p.target); ok {
			pairs = append(pairs, "backspace", "上级目录")
		}
	}
	if len(pairs) == 0 {
		return ""
	}
	var lines []string
	line := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		hint := theme.Key(pairs[i]) + " " + theme.SubtleStyle.Render(pairs[i+1])
		if line != "" && lipgloss.Width(line)+lipgloss.Width(hint)+3 > w {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += hint
	}
	lines = append(lines, line)
	return strings.Join(lines[:min(2, len(lines))], "\n")
}

// columnWidths 固定列优先，弹性列平分剩余宽度；太窄时从右侧依次隐藏列
func columnWidths(columns []systemColumn, w int) []int {
	for n := len(columns); n > 0; n-- {
		cols := columns[:n]
		fixed, flex := 0, 0
		for _, c := range cols {
			if c.width > 0 {
				fixed += c.width
			} else {
				flex++
			}
		}
		rest := w - fixed - (n - 1)
		if rest < 10*max(1, flex) && n > 1 {
			continue
		}
		widths := make([]int, n)
		share := 0
		for i, c := range cols {
			widths[i] = c.width
			if c.width == 0 {
				share++
				widths[i] = rest / flex
				if share == flex {
					widths[i] = rest - rest/flex*(flex-1)
				}
			}
		}
		return widths
	}
	return []int{w}
}

func (p *systemPage) listPane(w, h int) string {
	inner := max(1, widget.PanelInner(w))
	title := p.spec.tabs[p.tab]
	if len(p.spec.views) > 0 {
		title += " · " + p.spec.views[p.view]
	}
	title = theme.BoldStyle.Render(fmt.Sprintf("%s · %d/%d", title, len(p.visible), len(p.result.rows)))
	if p.loading {
		title += theme.MutedStyle.Render(" · 正在读取…")
	}
	lines := []string{theme.Truncate(title, inner)}
	if p.inputMode == "filter" {
		p.input.Width = max(1, inner-2)
		p.input.PromptStyle = theme.Fg(theme.Accent)
		lines = append(lines, p.input.View())
	} else if p.filter != "" {
		lines = append(lines, theme.Truncate(theme.Key("/")+" "+theme.SubtleStyle.Render(p.filter), inner))
	} else {
		lines = append(lines, theme.Truncate(theme.Key("/")+" "+theme.MutedStyle.Render("模糊查询"), inner))
	}
	columns := []systemColumn{{title: "名称"}}
	if p.spec.columns != nil {
		columns = p.spec.columns(p.tab)
	}
	widths := columnWidths(columns, inner-2)
	render := func(prefix string, values []string, row *systemRow, style lipgloss.Style) string {
		parts := []string{prefix}
		for i, width := range widths {
			c := columns[i]
			if i > 0 {
				parts = append(parts, " ")
			}
			if c.bar && row != nil {
				if row.ratio < 0 {
					parts = append(parts, strings.Repeat(" ", width))
				} else {
					parts = append(parts, widget.Bar(width, row.ratio))
				}
				continue
			}
			text, cellStyle := "", style
			if i < len(values) {
				text = resourceText(values[i])
			}
			if row != nil && i > 0 {
				cellStyle = theme.SubtleStyle
				if i == 1 && row.tone != nil {
					cellStyle = theme.Fg(row.tone)
				}
			}
			parts = append(parts, widget.Cell(strings.ReplaceAll(text, "\n", " "), width, cellStyle, c.right))
		}
		return strings.Join(parts, "")
	}
	titles := make([]string, len(columns))
	for i, c := range columns {
		titles[i] = c.title
	}
	lines = append(lines, render("  ", titles, nil, theme.MutedStyle), theme.Rule(inner))
	p.rows = max(1, h-4-len(lines))
	p.offset = widget.Scroll(p.cursor, p.offset, len(p.visible), p.rows)
	for i := p.offset; i < min(len(p.visible), p.offset+p.rows); i++ {
		row := p.visible[i]
		prefix, style := "  ", theme.TextStyle
		if i == p.cursor {
			prefix, style = theme.Fg(theme.Accent).Render("▌")+" ", theme.Fg(theme.Accent).Bold(true)
		}
		text := render(prefix, append([]string{row.name}, row.cells...), &row, style)
		if i == p.cursor && p.focus == 0 {
			text = lipgloss.NewStyle().Background(theme.Surface).Width(inner).Render(text)
		}
		lines = append(lines, theme.Truncate(text, inner))
	}
	if len(p.visible) == 0 {
		label := "暂无条目"
		switch {
		case p.loading:
			label = "正在读取，请稍候…"
		case len(p.result.rows) > 0:
			label = "没有匹配的条目，按 f 切换视图或 esc 清除搜索"
		}
		lines = append(lines, "", theme.MutedStyle.Render(label))
	}
	border := theme.Faint
	if p.focus == 0 {
		border = theme.Accent
	}
	return widget.Panel(w, h, border, strings.Join(lines, "\n"))
}

func (p *systemPage) detailPane(w, h int) string {
	inner := max(1, widget.PanelInner(w))
	title, content := "详情", theme.MutedStyle.Render("选择左侧条目查看详情")
	if p.output != "" {
		title = p.outputTitle + " · esc 关闭"
		content = ansi.Hardwrap(p.output, inner, true)
	} else if row, ok := p.current(); ok {
		title = resourceText(row.name)
		if len(row.fields) > 0 {
			content = fieldTable(row.fields, inner)
		}
	}
	lines := strings.Split(content, "\n")
	p.detailRows = len(lines)
	rows := max(1, h-7)
	p.detailView = rows
	p.detailOffset = max(0, min(p.detailOffset, len(lines)-rows))
	view := []string{theme.Truncate(theme.BoldStyle.Render(title), inner)}
	view = append(view, lines[p.detailOffset:min(len(lines), p.detailOffset+rows)]...)
	if len(lines) > rows {
		for len(view) < rows+1 {
			view = append(view, "")
		}
		hint := "→ 切换到此面板后 ↑↓ 滚动"
		if p.focus == 1 {
			hint = "↑↓ 滚动 · PgUp/PgDn 翻页"
		}
		view = append(view, theme.Truncate(theme.MutedStyle.Render(fmt.Sprintf("%s · %d–%d / %d 行", hint, p.detailOffset+1, min(len(lines), p.detailOffset+rows), len(lines))), inner))
	}
	border := theme.Faint
	if p.focus == 1 {
		border = theme.Accent
	}
	return widget.Panel(w, h, border, strings.Join(view, "\n"))
}
