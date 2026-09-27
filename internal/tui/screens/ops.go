package screens

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// 通用操作页：左侧状态面板（可定时刷新），右侧可用操作与执行日志。
// 适用于“查看状态 → 选择操作 → 确认 → 执行命令”一类的工具。

// Action 一个可执行的操作
type Action struct {
	Key     string
	Label   string
	Desc    string // 操作列表里的说明
	Confirm string // 确认弹窗里的详细说明
	Sudo    bool   // 需要管理员权限，执行前通过 sudo -v 缓存凭据
	Danger  bool
	Run     func(ctx context.Context, r sysx.Runner) []sysx.Step
}

// OpsTool 操作页的数据来源，S 为状态快照类型
type OpsTool[S any] interface {
	Crumbs() []string
	// Load 读取状态；只读操作，演练模式下也真实执行
	Load(ctx context.Context, r sysx.Runner) (S, error)
	// Render 渲染状态面板内容
	Render(s S, w, h int) string
	// Actions 当前状态下可用的操作
	Actions(s S) []Action
	// Interval 自动刷新间隔，0 表示只在操作后和手动刷新时读取
	Interval() time.Duration
}

type (
	opsLoadedMsg struct {
		page  any
		state any
		err   error
	}
	opsTickMsg struct{ page any }
	opsSudoMsg struct {
		page any
		err  error
	}
	opsDoneMsg struct {
		page  any
		steps []sysx.Step
	}
)

type opsPage[S any] struct {
	tool   OpsTool[S]
	runner sysx.Runner
	dryRun bool

	state   S
	loaded  bool
	loading bool
	err     error

	spin      spinner.Model
	confirm   *Action
	focusOK   bool
	running   *Action
	lastLabel string
	steps     []sysx.Step
}

// NewOps 基于工具创建操作页
func NewOps[S any](tool OpsTool[S], env Env) Page {
	return &opsPage[S]{
		tool:   tool,
		runner: sysx.RunnerFor(env.DryRun),
		dryRun: env.DryRun,
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *opsPage[S]) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.load()) }

func (m *opsPage[S]) load() tea.Cmd {
	m.loading = true
	tool, r := m.tool, m.runner
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s, err := tool.Load(ctx, r)
		return opsLoadedMsg{m, s, err}
	}
}

func (m *opsPage[S]) Busy() bool { return m.running != nil }

func (m *opsPage[S]) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd
	case opsLoadedMsg:
		if msg.page != m {
			return nil
		}
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.state, m.loaded = msg.state.(S), true
		}
		if d := m.tool.Interval(); d > 0 {
			return tea.Tick(d, func(time.Time) tea.Msg { return opsTickMsg{m} })
		}
	case opsTickMsg:
		// 执行操作或弹窗期间暂停刷新，避免状态在确认时跳变
		if msg.page == m && m.running == nil && m.confirm == nil && !m.loading {
			return m.load()
		}
		if msg.page == m {
			return tea.Tick(time.Second, func(time.Time) tea.Msg { return opsTickMsg{m} })
		}
	case opsSudoMsg:
		if msg.page != m {
			return nil
		}
		if msg.err != nil {
			m.steps = []sysx.Step{{Title: "获取管理员权限", Err: msg.err}}
			m.running = nil
			return nil
		}
		return m.execute()
	case opsDoneMsg:
		if msg.page != m {
			return nil
		}
		m.steps, m.running = msg.steps, nil
		return m.load()
	case tea.KeyMsg:
		return m.onKey(msg.String())
	}
	return nil
}

func (m *opsPage[S]) onKey(k string) tea.Cmd {
	if m.running != nil {
		return nil
	}
	if m.confirm != nil {
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.focusOK = !m.focusOK
		case "y":
			return m.start()
		case "enter":
			if m.focusOK {
				return m.start()
			}
			m.confirm = nil
		case "esc", "n", "q":
			m.confirm = nil
		}
		return nil
	}
	switch k {
	case "esc", "q":
		return Back
	case "r":
		if !m.loading {
			return m.load()
		}
		return nil
	}
	if !m.loaded {
		return nil
	}
	for _, a := range m.tool.Actions(m.state) {
		if a.Key == k {
			a := a
			m.confirm, m.focusOK = &a, false
		}
	}
	return nil
}

// start 确认后执行：需要管理员权限且凭据未缓存时，先暂停界面让 sudo 在终端里询问密码
func (m *opsPage[S]) start() tea.Cmd {
	m.running, m.confirm = m.confirm, nil
	m.lastLabel = m.running.Label
	m.steps = nil
	if m.running.Sudo && !m.dryRun && !sysx.SudoCached(context.Background()) {
		return tea.ExecProcess(exec.Command("sudo", "-v", "-p", "sysbox 需要管理员权限，请输入登录密码："),
			func(err error) tea.Msg { return opsSudoMsg{m, err} })
	}
	return tea.Batch(m.spin.Tick, m.execute())
}

func (m *opsPage[S]) execute() tea.Cmd {
	a, r := m.running, m.runner
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return opsDoneMsg{m, a.Run(ctx, r)}
	}
}

// ---------- 渲染 ----------

func (m *opsPage[S]) Crumbs() []string { return m.tool.Crumbs() }

func (m *opsPage[S]) Hints() []string {
	if m.confirm != nil {
		return []string{"←→", "切换", "enter", "确定", "esc", "取消"}
	}
	var hints []string
	if m.loaded {
		for _, a := range m.tool.Actions(m.state) {
			hints = append(hints, a.Key, a.Label)
		}
	}
	return append(hints, "r", "刷新", "esc", "返回", "t", "主题")
}

func (m *opsPage[S]) Status() string {
	switch {
	case m.running != nil:
		return m.spin.View() + " " + theme.SubtleStyle.Render("正在"+m.running.Label+"…")
	case m.loading:
		return m.spin.View() + " " + theme.MutedStyle.Render("刷新中")
	case m.tool.Interval() > 0:
		return theme.MutedStyle.Render("每 " + m.tool.Interval().String() + " 自动刷新")
	}
	return ""
}

func (m *opsPage[S]) Body(w, h int) string {
	m.spin.Style = theme.Fg(theme.Accent)
	iw, ih := w-4, h-1
	var body string
	if iw >= 100 {
		lw := iw * 58 / 100
		rw := iw - lw - 2
		actH := m.actionsHeight(rw)
		right := lipgloss.JoinVertical(lipgloss.Left, m.viewActions(rw, actH), m.viewLog(rw, ih-actH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.viewStatus(lw, ih), "  ", right)
	} else {
		logH := min(10, max(5, ih/3))
		body = lipgloss.JoinVertical(lipgloss.Left, m.viewStatus(iw, ih-logH), m.viewLog(iw, logH))
	}
	body = "\n" + widget.Inset(body)
	if m.confirm != nil {
		return widget.Overlay(body, m.viewConfirm(w), w, h)
	}
	return body
}

func (m *opsPage[S]) viewStatus(w, h int) string {
	inner := widget.PanelInner(w)
	var content string
	switch {
	case m.err != nil:
		content = theme.Fg(theme.Rose).Render("✗ 读取状态失败") + "\n\n" + theme.SubtleStyle.Width(inner).Render(m.err.Error())
	case !m.loaded:
		content = m.spin.View() + " " + theme.SubtleStyle.Render("正在读取状态…")
	default:
		content = m.tool.Render(m.state, inner, h-4)
	}
	return widget.Panel(w, h, theme.Faint, content)
}

func (m *opsPage[S]) actionsHeight(w int) int {
	if !m.loaded {
		return 5
	}
	n := len(m.tool.Actions(m.state))
	return max(5, n*2+4+1)
}

func (m *opsPage[S]) viewActions(w, h int) string {
	inner := widget.PanelInner(w)
	lines := []string{widget.Section("可用操作")}
	if m.loaded {
		actions := m.tool.Actions(m.state)
		if len(actions) == 0 {
			lines = append(lines, theme.MutedStyle.Render("当前状态下没有可执行的操作"))
		}
		for _, a := range actions {
			label := theme.BoldStyle.Render(a.Label)
			if a.Danger {
				label = theme.Fg(theme.Rose).Bold(true).Render(a.Label)
			}
			if a.Sudo {
				label += theme.MutedStyle.Render("  需要管理员权限")
			}
			lines = append(lines, theme.Key(a.Key)+" "+label,
				"    "+theme.Truncate(theme.MutedStyle.Render(a.Desc), inner-4))
		}
	}
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

func (m *opsPage[S]) viewLog(w, h int) string {
	inner := widget.PanelInner(w)
	title := "执行记录"
	if m.lastLabel != "" {
		title += " · " + m.lastLabel
	}
	lines := []string{widget.Section(title)}
	switch {
	case m.running != nil:
		lines = append(lines, m.spin.View()+" "+theme.SubtleStyle.Render("执行中…"))
	case len(m.steps) == 0:
		lines = append(lines, theme.MutedStyle.Render("还没有执行任何操作"))
	default:
		if m.dryRun {
			lines = append(lines, dryRunNote())
		}
		lines = append(lines, widget.StepLog(m.steps, inner, h-4-len(lines)))
	}
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

func (m *opsPage[S]) viewConfirm(bw int) string {
	a := m.confirm
	w := widget.DialogWidth(64, bw)
	inner := widget.DialogInner(w)
	var accent lipgloss.TerminalColor = theme.Accent
	if a.Danger {
		accent = theme.Rose
	}
	parts := []string{theme.BoldStyle.Render(a.Label), "", theme.SubtleStyle.Width(inner).Render(a.Confirm)}
	if a.Sudo && !m.dryRun {
		parts = append(parts, "", theme.MutedStyle.Width(inner).Render("需要管理员权限，确认后会在终端里询问登录密码"))
	}
	if m.dryRun {
		parts = append(parts, "", dryRunNote())
	}
	action := "确定"
	if m.dryRun {
		action = "开始演练"
	}
	parts = append(parts, "", widget.Buttons(inner,
		widget.Button("取消", !m.focusOK, theme.Subtle), widget.Button(action, m.focusOK, accent)))
	return widget.Dialog(accent, w, strings.Join(parts, "\n"))
}
