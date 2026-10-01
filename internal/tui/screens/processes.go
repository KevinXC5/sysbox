package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/tools/processes"
	"github.com/KevinXC5/sysbox/internal/ui/search"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

type processSnapshotMsg struct {
	snapshot processes.Snapshot
	err      error
}
type processTickMsg struct{ generation uint64 }
type processDetailsMsg struct {
	identity   string
	generation uint64
	details    processes.Details
	err        error
}
type processActionMsg struct{ result string }

type processPage struct {
	env                                         Env
	provider                                    processes.Provider
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	snapshot                                    processes.Snapshot
	items                                       []processes.Process
	cursor, offset, rows, detailRows            int
	detailOffsets                               [3]int
	detailFocus                                 int
	modal                                       *processes.Process
	filter, detailFilter, inputMode             string
	input                                       textinput.Model
	sortKey                                     int
	descending, paused, loading, acting, reveal bool
	interval                                    time.Duration
	timerGeneration, detailGeneration           uint64
	details                                     processes.Details
	detailIdentity                              string
	detailLoading                               bool
	detailCancel                                context.CancelFunc
	selected                                    map[string]processes.Process
	confirm                                     []processes.Process
	force                                       bool
	note                                        string
	lastRefresh                                 time.Time
}

func NewProcesses(env Env) Page { return newProcessPage(env, processes.NewProvider()) }
func newProcessPage(env Env, provider processes.Provider) *processPage {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.Prompt = "/ "
	input.CharLimit = 256
	return &processPage{env: env, provider: provider, ctx: ctx, cancel: cancel, input: input, cursor: -1, descending: true, interval: 2 * time.Second, selected: make(map[string]processes.Process)}
}
func (p *processPage) Close()           { p.cancel() }
func (p *processPage) Init() tea.Cmd    { return p.refresh() }
func (p *processPage) Busy() bool       { return p.acting }
func (p *processPage) Typing() bool     { return p.inputMode != "" || len(p.confirm) > 0 }
func (p *processPage) Crumbs() []string { return []string{"系统", "进程管理"} }
func (p *processPage) Hints() []string {
	if len(p.confirm) > 0 {
		return []string{"y", "确认结束", "esc", "取消"}
	}
	if p.inputMode != "" {
		return []string{"enter", "完成查询", "esc", "清空查询", "↑↓", "选择 / 滚动"}
	}
	if p.modal != nil {
		return []string{"←→", "切换列", "↑↓", "滚动当前列", "f", "查询详情", "v", "显示/隐藏环境值", "esc", "关闭弹窗"}
	}
	return []string{"↑↓", "选择", "enter", "查看详情", "esc", "取消选择 / 返回"}
}
func (p *processPage) Status() string {
	state := fmt.Sprintf("每 %s 刷新", p.interval)
	if p.paused {
		state = "自动刷新已暂停"
	}
	if p.loading {
		state = "正在刷新…"
	}
	return fmt.Sprintf("%d 个进程 · %s", len(p.items), state)
}
func (p *processPage) current() (processes.Process, bool) {
	if p.modal != nil {
		return *p.modal, true
	}
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return processes.Process{}, false
	}
	return p.items[p.cursor], true
}
func (p *processPage) refresh() tea.Cmd {
	if p.loading || p.acting || p.ctx.Err() != nil {
		return nil
	}
	p.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, 12*time.Second)
		defer cancel()
		s, e := p.provider.Snapshot(ctx)
		return processSnapshotMsg{s, e}
	}
}
func (p *processPage) tick() tea.Cmd {
	p.timerGeneration++
	generation := p.timerGeneration
	if p.paused || p.ctx.Err() != nil {
		return nil
	}
	return tea.Tick(p.interval, func(time.Time) tea.Msg { return processTickMsg{generation} })
}

var processSortKeys = []string{"cpu", "memory", "name", "pid", "port"}
var processSortLabels = []string{"CPU", "内存", "名称", "PID", "端口"}

// 列表按当前排序实时更新，光标保持行位置；弹窗单独绑定进程身份。
func (p *processPage) rebuild(_ string) {
	p.items = processes.Filter(p.snapshot.Processes, p.filter)
	processes.Sort(p.items, processSortKeys[p.sortKey], p.descending)
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	if p.modal != nil {
		for _, item := range p.snapshot.Processes {
			if processes.Identity(item) == processes.Identity(*p.modal) {
				*p.modal = item
				break
			}
		}
	}
}
func (p *processPage) loadDetails(force bool) tea.Cmd {
	if p.modal == nil {
		return nil
	}
	item, ok := p.current()
	identity := ""
	if ok {
		identity = processes.Identity(item)
	}
	if identity == p.detailIdentity && !force {
		return nil
	}
	if p.detailCancel != nil {
		p.detailCancel()
	}
	p.detailGeneration++
	generation := p.detailGeneration
	if identity != p.detailIdentity {
		p.detailOffsets = [3]int{}
		p.reveal = false
	}
	p.detailIdentity = identity
	p.details = processes.Details{}
	p.detailLoading = ok
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(p.ctx, 8*time.Second)
	p.detailCancel = cancel
	return func() tea.Msg {
		defer cancel()
		d, e := p.provider.Details(ctx, item)
		return processDetailsMsg{identity, generation, d, e}
	}
}
func (p *processPage) move(delta int) tea.Cmd {
	if p.modal != nil || len(p.confirm) > 0 {
		p.detailOffsets[p.detailFocus] = max(0, p.detailOffsets[p.detailFocus]+delta)
		return nil
	}
	if len(p.items) == 0 {
		return nil
	}
	if p.cursor < 0 {
		p.cursor = 0
	} else {
		p.cursor = max(0, min(len(p.items)-1, p.cursor+delta))
	}
	return nil
}
func (p *processPage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case processSnapshotMsg:
		p.loading = false
		item, _ := p.current()
		identity := processes.Identity(item)
		if msg.err != nil {
			p.note = "刷新失败：" + msg.err.Error()
		} else {
			p.snapshot = msg.snapshot
			p.lastRefresh = time.Now()
			p.rebuild(identity)
			// 退出的进程不能留在批量操作中；身份包含启动时间，防止 PID 重用。
			alive := make(map[string]bool)
			for _, v := range p.snapshot.Processes {
				alive[processes.Identity(v)] = true
			}
			for key := range p.selected {
				if !alive[key] {
					delete(p.selected, key)
				}
			}
		}
		return tea.Batch(p.tick(), p.loadDetails(false))
	case processTickMsg:
		if msg.generation != p.timerGeneration || p.paused {
			return nil
		}
		if p.acting {
			return p.tick()
		}
		return p.refresh()
	case processDetailsMsg:
		if msg.identity != p.detailIdentity || msg.generation != p.detailGeneration {
			return nil
		}
		p.detailLoading = false
		p.details = msg.details
		if msg.err != nil {
			p.details.Warning = "环境读取失败：" + msg.err.Error()
		}
		return nil
	case processActionMsg:
		p.acting = false
		p.note = msg.result
		p.selected = make(map[string]processes.Process)
		return p.refresh()
	case tea.KeyMsg:
		key := msg.String()
		if p.acting {
			return nil
		}
		if len(p.confirm) > 0 {
			if key == "esc" {
				p.confirm = nil
				return nil
			}
			if key == "y" {
				return p.terminate()
			}
			return nil
		}
		if p.inputMode != "" {
			if key == "esc" || key == "enter" {
				if key == "esc" {
					if p.inputMode == "list" {
						p.filter = ""
						p.rebuild("")
					} else {
						p.detailFilter = ""
					}
				}
				p.inputMode = ""
				p.input.Blur()
				return p.loadDetails(false)
			}
			if key == "up" {
				return p.move(-1)
			}
			if key == "down" {
				return p.move(1)
			}
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			if p.inputMode == "list" {
				item, _ := p.current()
				p.filter = p.input.Value()
				p.rebuild(processes.Identity(item))
				return tea.Batch(cmd, p.loadDetails(false))
			}
			p.detailFilter = p.input.Value()
			p.detailOffsets = [3]int{}
			return cmd
		}
		switch key {
		case "esc", "q":
			if p.modal != nil {
				p.modal = nil
				p.cursor = -1
				p.reveal = false
				p.detailFilter = ""
				if p.detailCancel != nil {
					p.detailCancel()
				}
				p.detailGeneration++
				p.detailIdentity = ""
				return nil
			}
			if p.cursor >= 0 || len(p.selected) > 0 {
				p.cursor = -1
				clear(p.selected)
				return nil
			}
			return Back
		case "enter":
			if p.modal == nil {
				if item, ok := p.current(); ok {
					p.modal = &item
					p.detailFocus = 0
					p.detailOffsets = [3]int{}
					return p.loadDetails(true)
				}
			}
		case "left":
			if p.modal != nil {
				p.detailFocus = max(0, p.detailFocus-1)
			}
		case "right":
			if p.modal != nil {
				p.detailFocus = min(2, p.detailFocus+1)
			}
		case "up":
			return p.move(-1)
		case "down":
			return p.move(1)
		case "pgup", "ctrl+u":
			rows := p.rows
			if p.modal != nil {
				rows = p.detailRows
			}
			return p.move(-max(1, rows/2))
		case "pgdown", "ctrl+d":
			rows := p.rows
			if p.modal != nil {
				rows = p.detailRows
			}
			return p.move(max(1, rows/2))
		case "/", "f":
			p.inputMode = "list"
			value := p.filter
			if key == "f" || p.modal != nil {
				p.inputMode = "detail"
				value = p.detailFilter
				if p.modal == nil && key == "f" {
					p.inputMode = ""
					return nil
				}
			}
			p.input.SetValue(value)
			return p.input.Focus()
		case "s":
			item, _ := p.current()
			p.sortKey = (p.sortKey + 1) % len(processSortKeys)
			p.rebuild(processes.Identity(item))
		case "S":
			item, _ := p.current()
			p.descending = !p.descending
			p.rebuild(processes.Identity(item))
		case "n":
			if p.modal == nil {
				return Open("network")
			}
		case "r":
			return tea.Batch(p.refresh(), p.loadDetails(true))
		case "p":
			p.paused = !p.paused
			return p.tick()
		case "i":
			switch p.interval {
			case time.Second:
				p.interval = 2 * time.Second
			case 2 * time.Second:
				p.interval = 5 * time.Second
			default:
				p.interval = time.Second
			}
			return p.tick()
		case "v":
			if p.modal != nil {
				p.reveal = !p.reveal
			}
		case "space":
			if item, ok := p.current(); ok {
				k := processes.Identity(item)
				if _, has := p.selected[k]; has {
					delete(p.selected, k)
				} else {
					p.selected[k] = item
				}
			}
		case "d", "D":
			p.force = key == "D"
			p.confirm = nil
			p.detailOffsets = [3]int{}
			for _, item := range p.snapshot.Processes {
				if _, ok := p.selected[processes.Identity(item)]; ok {
					p.confirm = append(p.confirm, item)
				}
			}
			if len(p.confirm) == 0 {
				if item, ok := p.current(); ok {
					p.confirm = []processes.Process{item}
				}
			}
		}
	}
	return nil
}
func (p *processPage) terminate() tea.Cmd {
	targets := append([]processes.Process(nil), p.confirm...)
	force := p.force
	p.confirm = nil
	p.acting = true
	return func() tea.Msg {
		var results []string
		for _, item := range targets {
			label := fmt.Sprintf("%s (%d)", item.Name, item.PID)
			if p.env.DryRun {
				results = append(results, "演练：将结束 "+label)
				continue
			}
			ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			err := p.provider.Terminate(ctx, item, force)
			cancel()
			if err != nil {
				results = append(results, label+"："+err.Error())
			} else {
				results = append(results, "已发送结束请求："+label)
			}
		}
		return processActionMsg{strings.Join(results, "\n")}
	}
}

func processInline(value string) string {
	return strings.Join(strings.Fields(resourceText(value)), " ")
}
func (p *processPage) Body(w, h int) string {
	// 详情弹窗直接覆盖主体区域，仅留一列外边距，充分使用终端空间。
	if p.modal != nil || len(p.confirm) > 0 {
		return lipgloss.NewStyle().PaddingLeft(1).Render(p.processDetailPane(max(1, w-2), h))
	}
	iw := max(1, w-2)
	direction := "↓"
	if !p.descending {
		direction = "↑"
	}
	head := fmt.Sprintf("排序：%s %s · %s · 已选 %d", processSortLabels[p.sortKey], direction, p.Status(), len(p.selected))
	hints := theme.HintsFit(iw, "enter", "详情", "/", "搜索", "s", "排序", "S", "升降序", "p", "暂停刷新", "i", "间隔", "r", "刷新", "n", "网络诊断", "space", "多选", "d", "结束", "D", "强制结束")
	top := theme.Truncate(head, iw) + "\n" + hints
	return lipgloss.NewStyle().PaddingLeft(1).Render(top + "\n" + p.listPane(iw, max(5, h-3)))
}
func (p *processPage) listPane(w, h int) string {
	inner := max(1, w-4)
	lines := []string{}
	if p.inputMode == "list" {
		p.input.Width = max(1, inner-2)
		lines = append(lines, p.input.View())
	} else {
		label := "/ 搜索进程名、PID、命令与端口"
		if p.filter != "" {
			label = "/ " + p.filter
		}
		lines = append(lines, theme.Truncate(label, inner))
	}
	pidW, cpuW, memW := 7, 8, 10
	portsW := max(10, inner/4)
	userW := 0
	if inner >= 90 {
		userW = 16
	}
	nameW := max(8, inner-3-pidW-cpuW-memW-portsW-userW)
	row := func(mark, pid, name, cpu, memory, user, ports string, style lipgloss.Style) string {
		return theme.Truncate(mark+widget.Cell(pid, pidW, style, false)+widget.Cell(name, nameW, style, false)+widget.Cell(cpu, cpuW, style, true)+widget.Cell(memory, memW, style, true)+widget.Cell(user, userW, style, false)+widget.Cell(ports, portsW, style, false), inner)
	}
	lines = append(lines, row("   ", "PID", "名称", "CPU", "内存", " 用户", " 监听端口", theme.MutedStyle), theme.Rule(inner))
	notice := p.note
	if notice == "" {
		notice = p.snapshot.Warning
	}
	reserved := 0
	if notice != "" {
		reserved = 1
	}
	p.rows = max(1, h-2-len(lines)-reserved)
	if p.cursor >= 0 {
		p.offset = widget.Scroll(p.cursor, p.offset, len(p.items), p.rows)
	} else {
		p.offset = max(0, min(p.offset, max(0, len(p.items)-p.rows)))
	}
	for i := p.offset; i < min(len(p.items), p.offset+p.rows); i++ {
		item := p.items[i]
		mark := " "
		if _, ok := p.selected[processes.Identity(item)]; ok {
			mark = "✓"
		}
		style := theme.TextStyle
		prefix := " " + mark + " "
		if i == p.cursor {
			prefix = "▌" + mark + " "
			style = theme.Fg(theme.Accent).Bold(true)
		}
		text := row(prefix, fmt.Sprint(item.PID), processInline(item.Name), fmt.Sprintf("%.1f%% ", item.CPU), fsx.FormatBytes(int64(item.Memory))+" ", " "+processInline(item.User), " "+processes.PortSummary(item), style)
		if i == p.cursor {
			text = lipgloss.NewStyle().Background(theme.Surface).Width(inner).Render(text)
		}
		lines = append(lines, text)
	}
	if len(p.items) == 0 {
		label := "没有匹配的进程"
		if p.loading {
			label = "正在读取进程…"
		}
		lines = append(lines, label)
	}
	// 为操作结果和采集提示保留一行，进程数量较多时也能看到反馈。
	if notice != "" && len(lines) < h-2 {
		lines = append(lines, theme.Truncate(processInline(notice), inner))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Faint).Padding(0, 1).Width(max(1, w-2)).Height(max(1, h-2)).MaxHeight(h).Render(strings.Join(lines, "\n"))
}
func (p *processPage) detailLines(section int) []string {
	if len(p.confirm) > 0 {
		title := "确认结束以下进程？"
		if p.force {
			title = "确认强制结束以下进程？未保存内容可能丢失。"
		}
		lines := []string{title, "y 确认 · esc 取消"}
		for _, v := range p.confirm {
			lines = append(lines, fmt.Sprintf("%d %s", v.PID, processInline(v.Name)))
		}
		return lines
	}
	item, ok := p.current()
	if !ok {
		return []string{"请选择进程"}
	}
	switch section {
	case 1:
		lines := []string{"协议 · 本地地址 → 远端地址 · 状态"}
		for _, port := range item.Ports {
			line := port.Protocol + " " + port.Local
			if port.Remote != "" {
				line += " → " + port.Remote
			}
			line += " · " + port.State
			lines = append(lines, line)
		}
		if len(item.Ports) == 0 {
			lines = append(lines, "未发现端口或连接；部分进程可能无权限读取")
		}
		return lines
	case 2:
		if p.detailLoading {
			return []string{"正在读取环境变量…"}
		}
		lines := processes.EnvironmentLines(p.details, p.reveal, p.detailFilter)
		if p.details.Warning != "" {
			lines = append(lines, "", p.details.Warning)
		}
		return lines
	default:
		lines := []string{fmt.Sprintf("PID：%d   父进程：%d", item.PID, item.PPID), "名称：" + item.Name, "用户：" + item.User, fmt.Sprintf("CPU：%.1f%%   内存：%s", item.CPU, fsx.FormatBytes(int64(item.Memory))), "启动时间：" + item.Started, "路径：" + item.Path, "命令：" + item.Command}
		for _, parent := range p.snapshot.Processes {
			if parent.PID == item.PPID {
				lines = append(lines, fmt.Sprintf("父进程：%s (%d)", parent.Name, parent.PID))
				break
			}
		}
		children := []string{}
		for _, child := range p.snapshot.Processes {
			if child.PPID == item.PID {
				children = append(children, fmt.Sprintf("%s (%d)", child.Name, child.PID))
			}
		}
		if len(children) > 0 {
			lines = append(lines, "子进程："+strings.Join(children, "、"))
		}
		return lines
	}
}

// 三列同时呈现详情，列标题固定，长命令和环境变量在各自列内折行。
func (p *processPage) processDetailPane(w, h int) string {
	inner := max(1, w-4)
	top := []string{}
	if item, ok := p.current(); ok {
		top = append(top, theme.Truncate(theme.BoldStyle.Render(fmt.Sprintf("%s · PID %d · esc 关闭", processInline(item.Name), item.PID)), inner))
	}
	top = append(top, theme.Truncate("←→ 切换列 · ↑↓ 滚动当前列 · f 查询详情 · v 显示/隐藏环境值", inner))
	if p.inputMode == "detail" {
		p.input.Width = max(1, inner-2)
		top = append(top, p.input.View())
	}
	if len(p.confirm) > 0 {
		// 结束操作的确认独占弹窗，保证所有目标清晰可见。
		for _, line := range p.detailLines(0) {
			top = append(top, strings.Split(ansi.Hardwrap(resourceText(line), inner, true), "\n")...)
		}
	} else {
		available := max(3, inner-6)
		widths := []int{available / 3, available / 3, available - 2*(available/3)}
		titles := []string{"基本信息", "端口与连接", "环境变量"}
		cells := make([]string, 3)
		for i := range cells {
			style := theme.BoldStyle.Width(widths[i]).Align(lipgloss.Center)
			if i == p.detailFocus {
				style = style.Foreground(theme.OnColor).Background(theme.Accent)
			}
			// 标题文字居中，当前列的底色覆盖完整单元格。
			cells[i] = style.Render(theme.Truncate(titles[i], widths[i]))
		}
		separator := theme.MutedStyle.Render(" │ ")
		top = append(top, strings.Join(cells, separator), theme.Rule(inner))
		columns := make([][]string, 3)
		for i := range columns {
			raw := p.detailLines(i)
			if i == 0 {
				for _, notice := range []string{p.note, p.snapshot.Warning} {
					if notice != "" {
						raw = append(raw, "", notice)
					}
				}
			}
			for _, line := range raw {
				if p.detailFilter != "" && i != 2 {
					if _, ok := search.Score(p.detailFilter, line); !ok {
						continue
					}
				}
				columns[i] = append(columns[i], strings.Split(ansi.Hardwrap(resourceText(line), max(1, widths[i]), true), "\n")...)
			}
		}
		p.detailRows = max(1, h-2-len(top))
		visibleRows := 0
		for col := range columns {
			// 各列拥有独立的滚动位置，切换焦点和刷新不会改变其他列的位置。
			p.detailOffsets[col] = min(p.detailOffsets[col], max(0, len(columns[col])-p.detailRows))
			visibleRows = max(visibleRows, min(p.detailRows, len(columns[col])-p.detailOffsets[col]))
		}
		for row := 0; row < visibleRows; row++ {
			for col := range cells {
				index := p.detailOffsets[col] + row
				value := ""
				if index < len(columns[col]) {
					value = columns[col][index]
				}
				cells[col] = widget.Cell(value, widths[col], theme.TextStyle, false)
			}
			top = append(top, strings.Join(cells, separator))
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Accent).Padding(0, 1).Width(max(1, w-2)).Height(max(1, h-2)).MaxHeight(h).Render(strings.Join(top, "\n"))
}
