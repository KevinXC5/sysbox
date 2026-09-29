package screens

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/tools/claudeupdate"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// Claude Code 更新页：选择目标版本 → 下载（断点续传）→ 校验 → 调用官方安装

type claudeState int

const (
	claudeIdle claudeState = iota
	claudeRunning
	claudeDone
	claudeFailed
)

// 目标选项，最后一项为手动输入版本号
var claudeTargets = []struct{ key, label, desc string }{
	{"latest", "latest", "最新发布版本"},
	{"stable", "stable", "稳定通道版本"},
	{"", "指定版本", "输入形如 2.1.300 的版本号"},
}

type (
	claudeProxyMsg struct{ proxy, note string }
	claudeEventMsg claudeupdate.Event
	claudeDoneMsg  struct {
		res     claudeupdate.Result
		err     error
		elapsed time.Duration
	}
)

type claudePage struct {
	env     Env
	home    string
	current string

	proxy, proxyNote string
	proxyReady       bool

	state  claudeState
	choice int
	input  textinput.Model
	spin   spinner.Model

	ch      chan tea.Msg
	stage   claudeupdate.Stage
	logs    []claudeupdate.Event
	done    int64
	total   int64
	speed   float64 // 字节/秒，指数平滑
	lastAt  time.Time
	lastN   int64
	result  claudeupdate.Result
	err     error
	elapsed time.Duration // 安装结束时固定的实际用时
}

// NewClaude 创建 Claude Code 更新页
func NewClaude(env Env) Page {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return NewErrorPage([]string{"Claude Code"}, fmt.Errorf("无法确定用户主目录: %v", err))
	}
	in := textinput.New()
	in.Placeholder = "2.1.300"
	in.CharLimit = 40
	return &claudePage{
		env:     env,
		home:    home,
		current: claudeupdate.Current(home),
		input:   in,
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

// proxySetting 代理设置：环境变量优先于配置文件，兼容旧脚本的 PROXY_URL 与 CLAUDE_NO_PROXY
func (m *claudePage) proxySetting() (string, bool) {
	proxy := m.env.Config.Claude.Proxy
	if proxy == "" {
		proxy = claudeupdate.DefaultProxy
	}
	if v := os.Getenv("PROXY_URL"); v != "" {
		proxy = v
	}
	direct := m.env.Config.Claude.Direct || os.Getenv("CLAUDE_NO_PROXY") == "1"
	return proxy, direct
}

func (m *claudePage) Init() tea.Cmd {
	proxy, direct := m.proxySetting()
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		p, note := claudeupdate.ResolveProxy(proxy, direct)
		return claudeProxyMsg{p, note}
	})
}

func (m *claudePage) Busy() bool { return m.state == claudeRunning }

func (m *claudePage) target() string {
	if t := claudeTargets[m.choice].key; t != "" {
		return t
	}
	return strings.TrimSpace(m.input.Value())
}

func (m *claudePage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		// 代理检测和安装结束后停止动画计时，避免空闲页面继续刷新。
		if m.proxyReady && m.state != claudeRunning {
			return nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd
	case claudeProxyMsg:
		m.proxy, m.proxyNote, m.proxyReady = msg.proxy, msg.note, true
	case claudeEventMsg:
		m.onEvent(claudeupdate.Event(msg))
		return m.wait()
	case claudeDoneMsg:
		m.result, m.err, m.elapsed = msg.res, msg.err, msg.elapsed
		m.state = claudeDone
		if msg.err != nil {
			m.state = claudeFailed
		}
		m.current = claudeupdate.Current(m.home)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return nil
}

func (m *claudePage) onKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch m.state {
	case claudeRunning:
		return nil
	case claudeDone, claudeFailed:
		switch k {
		case "enter", "esc", "q":
			return Back
		case "r":
			m.state, m.logs, m.err = claudeIdle, nil, nil
		}
		return nil
	}
	// 输入版本号时，除导航键外都交给输入框
	if m.input.Focused() {
		switch k {
		case "up", "esc":
			m.input.Blur()
			if k == "up" {
				m.choice--
			}
			return nil
		case "enter":
			return m.start()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	switch k {
	case "up", "k":
		m.choice = max(0, m.choice-1)
	case "down", "j":
		m.choice = min(len(claudeTargets)-1, m.choice+1)
		if claudeTargets[m.choice].key == "" {
			return m.input.Focus()
		}
	case "enter":
		if claudeTargets[m.choice].key == "" {
			return m.input.Focus()
		}
		return m.start()
	case "esc", "q":
		return Back
	}
	return nil
}

// start 校验目标后在后台执行更新，事件经通道逐条送回界面
func (m *claudePage) start() tea.Cmd {
	target := m.target()
	if !claudeupdate.ValidTarget(target) || !m.proxyReady {
		return nil
	}
	m.input.Blur()
	m.state, m.logs, m.err = claudeRunning, nil, nil
	m.done, m.total, m.speed, m.lastN = 0, 0, 0, 0
	m.elapsed = 0
	m.lastAt = time.Now()
	started := m.lastAt
	m.ch = make(chan tea.Msg, 64)

	opt := claudeupdate.Defaults(m.home, m.proxy)
	opt.DryRun = m.env.DryRun
	ch := m.ch
	go func() {
		u := claudeupdate.New(opt, func(e claudeupdate.Event) { ch <- claudeEventMsg(e) })
		res, err := u.Run(context.Background(), target)
		// 在后台任务真正结束时固定用时，不包含界面停留和消息处理时间。
		ch <- claudeDoneMsg{res: res, err: err, elapsed: time.Since(started)}
		close(ch)
	}()
	return tea.Batch(m.spin.Tick, m.wait())
}

func (m *claudePage) wait() tea.Cmd {
	ch := m.ch
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// onEvent 记录日志并按指数平滑估算下载速度
func (m *claudePage) onEvent(e claudeupdate.Event) {
	m.stage = e.Stage
	if e.Msg != "" {
		m.logs = append(m.logs, e)
	}
	if e.Total > 0 || e.Downloaded > 0 {
		now := time.Now()
		if dt := now.Sub(m.lastAt).Seconds(); dt > 0.2 && e.Downloaded >= m.lastN {
			inst := float64(e.Downloaded-m.lastN) / dt
			if m.speed == 0 {
				m.speed = inst
			} else {
				m.speed = m.speed*0.7 + inst*0.3
			}
			m.lastAt, m.lastN = now, e.Downloaded
		}
		m.done, m.total = e.Downloaded, e.Total
	}
}

// ---------- 渲染 ----------

func (m *claudePage) Crumbs() []string { return []string{"工具", "Claude Code 更新"} }

func (m *claudePage) Hints() []string {
	switch m.state {
	case claudeIdle:
		if m.input.Focused() {
			return []string{"enter", "开始", "↑", "返回选项", "esc", "取消输入"}
		}
		return []string{"↑↓", "选择", "enter", "开始", "esc", "返回", "t", "主题"}
	case claudeDone, claudeFailed:
		return []string{"enter", "返回首页", "r", "重新选择"}
	}
	return nil
}

func (m *claudePage) Status() string {
	if m.current == "" {
		return theme.MutedStyle.Render("未检测到已安装的版本")
	}
	return theme.MutedStyle.Render("当前版本 ") + theme.Fg(theme.Accent).Render(m.current)
}

func (m *claudePage) Body(w, h int) string {
	m.spin.Style = theme.Fg(theme.Accent)
	m.input.PromptStyle = theme.Fg(theme.Accent)
	m.input.TextStyle = theme.TextStyle
	m.input.PlaceholderStyle = theme.MutedStyle
	iw := w - 4
	if m.state == claudeIdle {
		return m.viewForm(iw, h)
	}
	return m.viewRun(iw, h)
}

func (m *claudePage) viewForm(iw, h int) string {
	lw := iw
	wide := iw >= 100
	if wide {
		lw = iw * 55 / 100
	}
	inner := widget.PanelInner(lw)
	lines := []string{widget.Section("选择目标版本"), ""}
	for i, t := range claudeTargets {
		mark, style := "  ", theme.TextStyle
		if i == m.choice {
			mark, style = theme.Fg(theme.Accent).Render("▌ "), theme.Fg(theme.Accent).Bold(true)
		}
		lines = append(lines, mark+widget.Cell(t.label, 12, style, false)+theme.MutedStyle.Render(t.desc))
	}
	if claudeTargets[m.choice].key == "" {
		m.input.Width = inner - 6
		lines = append(lines, "", "  "+m.input.View())
		if v := strings.TrimSpace(m.input.Value()); v != "" && !claudeupdate.ValidTarget(v) {
			lines = append(lines, "  "+theme.Fg(theme.Rose).Render("版本号格式不正确"))
		}
	}
	lines = append(lines, "", widget.Section("网络"))
	if m.proxyReady {
		ok := m.proxy != ""
		lines = append(lines, statusLine("连接方式", ok, m.proxyNote))
	} else {
		lines = append(lines, m.spin.View()+" "+theme.MutedStyle.Render("正在检测代理"))
	}
	if m.env.DryRun {
		lines = append(lines, "", dryRunNote())
	}
	form := widget.Panel(lw, h-1, theme.Faint, strings.Join(lines, "\n"))
	if !wide {
		return "\n" + widget.Inset(form)
	}
	return "\n" + widget.Inset(lipgloss.JoinHorizontal(lipgloss.Top, form, "  ", m.viewAbout(iw-lw-2, h-1)))
}

func (m *claudePage) viewAbout(w, h int) string {
	cur := theme.MutedStyle.Render("未安装")
	if m.current != "" {
		cur = theme.BoldStyle.Render(m.current)
	}
	lines := []string{
		widget.Section("当前安装"), "",
		widget.KV("版本", 8, cur),
		widget.KV("位置", 8, theme.SubtleStyle.Render("~/.local/share/claude/versions")),
		"", widget.Section("更新流程"),
		bullets([]string{
			"从官方发布地址读取目标版本和 SHA-256 校验值",
			"断点续传下载，网络中断自动重试，最多 11 次",
			"30 秒内速度低于 10KB/s 视为卡住，自动重连",
			"校验通过后调用新版本自带的 install 完成安装",
			"下载失败保留缓存，再次运行同一版本会接着下载",
		}, widget.PanelInner(w)),
	}
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

var claudeStageNames = map[claudeupdate.Stage]string{
	claudeupdate.StageResolve:  "解析版本",
	claudeupdate.StageManifest: "读取发布清单",
	claudeupdate.StageDownload: "下载",
	claudeupdate.StageInstall:  "安装",
	claudeupdate.StageDone:     "完成",
}

func (m *claudePage) viewRun(iw, h int) string {
	var title, right string
	pct := 0.0
	switch m.state {
	case claudeRunning:
		title = m.spin.View() + " " + theme.BoldStyle.Render("正在更新") + "  " + theme.SubtleStyle.Render(claudeStageNames[m.stage])
		if m.total > 0 {
			pct = float64(m.done) / float64(m.total)
			right = theme.Fg(theme.Accent2).Bold(true).Render(fsx.FormatBytes(m.done)) +
				theme.MutedStyle.Render(" / "+fsx.FormatBytes(m.total))
			if m.speed > 0 {
				eta := time.Duration(float64(m.total-m.done)/m.speed) * time.Second
				right += theme.MutedStyle.Render(fmt.Sprintf("   %s/s · 剩余 %s", fsx.FormatBytes(int64(m.speed)), duration(eta)))
			}
		}
		if m.stage >= claudeupdate.StageInstall {
			pct = 1
		}
	case claudeDone:
		pct = 1
		title = theme.Fg(theme.Green).Bold(true).Render("✓ ") + theme.BoldStyle.Render(m.doneTitle())
		right = theme.MutedStyle.Render(fmt.Sprintf("用时 %.1f 秒", m.elapsed.Seconds()))
	case claudeFailed:
		title = theme.Fg(theme.Rose).Bold(true).Render("✗ 更新失败")
		right = theme.MutedStyle.Render("已下载的部分会保留，重试时接着下载")
	}
	head := widget.ProgressHead(iw, title, right, pct)

	var lines []string
	for _, e := range m.logs {
		icon, st := theme.Fg(theme.Green).Render("✓ "), theme.SubtleStyle
		if e.Warn {
			icon, st = theme.Fg(theme.Amber).Render("! "), theme.Fg(theme.Amber)
		}
		lines = append(lines, theme.Truncate(icon+st.Render(e.Msg), iw))
	}
	if m.err != nil {
		lines = append(lines, "", theme.Fg(theme.Rose).Width(iw).Render("✗ "+m.err.Error()))
	}
	room := max(1, h-lipgloss.Height(head)-1)
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	return widget.Inset(head + "\n" + strings.Join(lines, "\n"))
}

func (m *claudePage) doneTitle() string {
	switch {
	case m.env.DryRun:
		return "演练完成：将更新到 " + m.result.Version
	case m.result.Cached:
		return "已安装 " + m.result.Version + "（复用本地已校验的文件）"
	}
	return "已安装 " + m.result.Version
}

// Typing 输入版本号时不拦截单字母快捷键
func (m *claudePage) Typing() bool { return m.input.Focused() }
