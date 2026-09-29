package screens

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/selfupdate"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// sysbox 自升级页：查询新版本并列出更新内容 → 下载校验 → 替换 → 提示重启

// RestartMsg 升级完成后请求用新版本重启
type RestartMsg struct{}

type upState int

const (
	upChecking upState = iota
	upReady
	upLatest
	upInstalling
	upDone
	upFailed
)

type (
	upReleaseMsg struct {
		pending []selfupdate.Release
		latest  string
		err     error
	}
	upProgressMsg struct{ done, total int64 }
	upDoneMsg     struct{ err error }
)

type selfUpdatePage struct {
	env    Env
	client *selfupdate.Client
	state  upState
	rel    selfupdate.Release   // 将要安装的版本
	notes  []selfupdate.Release // 当前版本之后的全部发布，从新到旧
	latest string               // 已是最新时显示的最新发布版本
	off    int                  // 更新内容的滚动偏移
	maxOff int
	err    error
	spin   spinner.Model
	ch     chan tea.Msg
	done   int64
	total  int64
}

// NewSelfUpdate 创建自升级页
func NewSelfUpdate(env Env) Page {
	return &selfUpdatePage{
		env:    env,
		client: selfupdate.NewClient(meta.Repo),
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *selfUpdatePage) Init() tea.Cmd {
	c := m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		pending, err := c.Pending(ctx, meta.Version)
		if err != nil || len(pending) > 0 {
			return upReleaseMsg{pending: pending, err: err}
		}
		rel, err := c.Latest(ctx)
		return upReleaseMsg{latest: rel.Tag, err: err}
	})
}

func (m *selfUpdatePage) Busy() bool { return m.state == upInstalling }

func (m *selfUpdatePage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.state != upChecking && m.state != upInstalling {
			return nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd
	case upReleaseMsg:
		m.notes, m.latest, m.err = msg.pending, msg.latest, msg.err
		switch {
		case msg.err != nil:
			m.state = upFailed
		case len(msg.pending) > 0:
			m.rel = msg.pending[0]
			m.state = upReady
		default:
			m.state = upLatest
		}
	case upProgressMsg:
		m.done, m.total = msg.done, msg.total
		return m.wait()
	case upDoneMsg:
		m.err = msg.err
		m.state = upDone
		if msg.err != nil {
			m.state = upFailed
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.off = max(0, m.off-1)
		case "down", "j":
			m.off = min(m.maxOff, m.off+1)
		case "pgup", "b":
			m.off = max(0, m.off-10)
		case "pgdown", "f", " ":
			m.off = min(m.maxOff, m.off+10)
		case "enter":
			switch m.state {
			case upReady:
				return m.install()
			case upDone:
				return func() tea.Msg { return RestartMsg{} }
			case upLatest, upFailed:
				return Back
			}
		case "esc", "q":
			if m.state != upInstalling {
				return Back
			}
		}
	}
	return nil
}

func (m *selfUpdatePage) install() tea.Cmd {
	if m.env.DryRun {
		m.state = upDone
		return nil
	}
	m.state = upInstalling
	m.ch = make(chan tea.Msg, 64)
	ch, c, rel := m.ch, m.client, m.rel
	go func() {
		defer close(ch)
		exe, err := selfupdate.Executable()
		if err == nil {
			err = c.Install(context.Background(), rel, exe, func(done, total int64) {
				select {
				case ch <- upProgressMsg{done, total}:
				default: // 界面来不及消费时丢弃中间进度
				}
			})
		}
		ch <- upDoneMsg{err}
	}()
	return tea.Batch(m.spin.Tick, m.wait())
}

func (m *selfUpdatePage) wait() tea.Cmd {
	ch := m.ch
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *selfUpdatePage) Crumbs() []string { return []string{"升级 sysbox"} }

func (m *selfUpdatePage) Hints() []string {
	switch m.state {
	case upReady:
		if m.maxOff > 0 {
			return []string{"↑↓", "滚动", "enter", "开始升级", "esc", "返回"}
		}
		return []string{"enter", "开始升级", "esc", "返回"}
	case upDone:
		if m.env.DryRun {
			return []string{"esc", "返回"}
		}
		return []string{"enter", "重启 sysbox", "esc", "稍后"}
	case upInstalling:
		return nil
	}
	return []string{"enter", "返回首页"}
}

func (m *selfUpdatePage) Status() string {
	return theme.MutedStyle.Render("当前版本 ") + theme.Fg(theme.Accent).Render(meta.Version)
}

func (m *selfUpdatePage) Body(w, h int) string {
	m.spin.Style = theme.Fg(theme.Accent)
	dw := widget.DialogWidth(64, w)
	if m.state == upReady {
		dw = widget.DialogWidth(80, w)
	}
	inner := widget.DialogInner(dw)
	var lines []string
	arrow := theme.SubtleStyle.Render(meta.Version) + theme.MutedStyle.Render("  →  ") + theme.Fg(theme.Green).Bold(true).Render(m.rel.Tag)
	switch m.state {
	case upChecking:
		lines = []string{m.spin.View() + " " + theme.SubtleStyle.Render("正在查询最新版本…")}
	case upLatest:
		lines = []string{theme.Fg(theme.Green).Bold(true).Render("✓ 已是最新版本"), "", theme.SubtleStyle.Render("当前 " + meta.Version + "，最新发布 " + m.latest)}
	case upReady:
		head := []string{theme.BoldStyle.Render("发现新版本"), "", arrow, "", widget.Section("更新内容")}
		foot := []string{"", theme.MutedStyle.Width(inner).Render("下载 " + selfupdate.AssetName() + "，校验 SHA-256 后原子替换当前可执行文件。")}
		if m.env.DryRun {
			foot = append(foot, "", dryRunNote())
		}
		// 对话框边框与上下内边距共占 4 行，剩下的高度留给更新内容，放不下时滚动
		lines = append(head, m.notesView(inner, h-4-len(head)-len(foot))...)
		lines = append(lines, foot...)
	case upInstalling:
		pct := 0.0
		if m.total > 0 {
			pct = float64(m.done) / float64(m.total)
		}
		lines = []string{m.spin.View() + " " + theme.BoldStyle.Render("正在下载 "+m.rel.Tag), "", widget.Bar(inner, pct), "",
			theme.MutedStyle.Render(fsx.FormatBytes(m.done) + " / " + fsx.FormatBytes(m.total))}
	case upDone:
		lines = []string{theme.Fg(theme.Green).Bold(true).Render("✓ 升级完成"), "", arrow, "", theme.SubtleStyle.Render("按 enter 用新版本重启")}
		if m.env.DryRun {
			lines = []string{theme.Fg(theme.Green).Bold(true).Render("✓ 演练完成"), "", arrow, "", dryRunNote()}
		}
	case upFailed:
		lines = []string{theme.Fg(theme.Rose).Bold(true).Render("✗ 升级失败"), "", theme.SubtleStyle.Width(inner).Render(m.err.Error())}
	}
	return widget.Center(w, h, widget.Dialog(theme.Faint, dw, strings.Join(lines, "\n")))
}

// notesView 更新内容区：逐个版本列出发布说明，超出 h 行时按滚动偏移截取并标出位置
func (m *selfUpdatePage) notesView(w, h int) []string {
	var all []string
	for i, r := range m.notes {
		if i > 0 {
			all = append(all, "")
		}
		all = append(all, theme.Fg(theme.Accent).Bold(true).Render(r.Tag))
		notes := selfupdate.Notes(r.Body)
		if len(notes) == 0 {
			notes = []string{"没有填写发布说明"}
		}
		for _, l := range notes {
			all = append(all, strings.Split(noteLine(l, w), "\n")...)
		}
	}
	h = max(3, h)
	if len(all) <= h {
		m.off, m.maxOff = 0, 0
		return all
	}
	// 留一行显示滚动位置
	h--
	m.maxOff = len(all) - h
	m.off = min(m.off, m.maxOff)
	view := append([]string{}, all[m.off:m.off+h]...)
	pos := theme.MutedStyle.Render("第 " + strconv.Itoa(m.off+1) + "–" + strconv.Itoa(m.off+h) + " 行，共 " + strconv.Itoa(len(all)) + " 行")
	return append(view, lipgloss.PlaceHorizontal(w, lipgloss.Right, pos))
}

// noteLine 渲染一行 Markdown 发布说明：标题加粗，列表项带圆点并悬挂缩进，其余按段落折行
func noteLine(l string, w int) string {
	switch {
	case strings.HasPrefix(l, "#"):
		return theme.BoldStyle.Render(strings.TrimSpace(strings.TrimLeft(l, "#")))
	case strings.HasPrefix(l, "- "), strings.HasPrefix(l, "* "):
		lines := wrapText(l[2:], w-2)
		for i, x := range lines {
			lead := "  "
			if i == 0 {
				lead = theme.Fg(theme.Accent).Render("· ")
			}
			lines[i] = lead + theme.TextStyle.Render(x)
		}
		return strings.Join(lines, "\n")
	}
	lines := wrapText(l, w)
	for i, x := range lines {
		lines[i] = theme.SubtleStyle.Render(x)
	}
	return strings.Join(lines, "\n")
}

// wrapText 中英混排的折行：英文按单词、中文按字断开，行首不留中文标点。
// lipgloss 只在空格处折行，会把整段中文当作一个长单词挪到下一行。
func wrapText(s string, w int) []string {
	// 切成不可再分的片段：连续的非空白 ASCII 为一个单词，其余字符各自成段，行首禁用的标点并入前一段
	var toks []string
	for _, r := range s {
		n := len(toks)
		switch {
		case r == ' ' || r == '\t':
			toks = append(toks, " ")
		case n > 0 && toks[n-1] != " " && (r < 0x80 && isASCIIWord(toks[n-1]) || strings.ContainsRune("，。、；：？！）》”’", r)):
			toks[n-1] += string(r)
		default:
			toks = append(toks, string(r))
		}
	}
	var lines []string
	cur := ""
	for _, t := range toks {
		if cur != "" && ansi.StringWidth(cur+t) > w {
			lines = append(lines, strings.TrimRight(cur, " "))
			cur = ""
		}
		if cur == "" && t == " " {
			continue
		}
		// 单个片段比整行还宽时硬折断
		for ansi.StringWidth(t) > w {
			head := ansi.Truncate(t, w, "")
			if head == "" {
				break
			}
			lines = append(lines, head)
			t = t[len(head):]
		}
		cur += t
	}
	return append(lines, strings.TrimRight(cur, " "))
}

// isASCIIWord 片段是否全部由 ASCII 字符组成
func isASCIIWord(t string) bool {
	for i := 0; i < len(t); i++ {
		if t[i] >= 0x80 {
			return false
		}
	}
	return true
}
