package screens

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/selfupdate"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// sysbox 自升级页：查询最新版本 → 下载校验 → 替换 → 提示重启

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
		rel selfupdate.Release
		err error
	}
	upProgressMsg struct{ done, total int64 }
	upDoneMsg     struct{ err error }
)

type selfUpdatePage struct {
	env    Env
	client *selfupdate.Client
	state  upState
	rel    selfupdate.Release
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
		rel, err := c.Latest(ctx)
		return upReleaseMsg{rel, err}
	})
}

func (m *selfUpdatePage) Busy() bool { return m.state == upInstalling }

func (m *selfUpdatePage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd
	case upReleaseMsg:
		m.rel, m.err = msg.rel, msg.err
		switch {
		case msg.err != nil:
			m.state = upFailed
		case selfupdate.Newer(msg.rel.Tag, meta.Version):
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
	inner := widget.DialogInner(dw)
	var lines []string
	arrow := theme.SubtleStyle.Render(meta.Version) + theme.MutedStyle.Render("  →  ") + theme.Fg(theme.Green).Bold(true).Render(m.rel.Tag)
	switch m.state {
	case upChecking:
		lines = []string{m.spin.View() + " " + theme.SubtleStyle.Render("正在查询最新版本…")}
	case upLatest:
		lines = []string{theme.Fg(theme.Green).Bold(true).Render("✓ 已是最新版本"), "", theme.SubtleStyle.Render("当前 " + meta.Version + "，最新发布 " + m.rel.Tag)}
	case upReady:
		lines = []string{theme.BoldStyle.Render("发现新版本"), "", arrow, "",
			theme.MutedStyle.Width(inner).Render("下载 " + selfupdate.AssetName() + "，校验 SHA-256 后原子替换当前可执行文件。")}
		if m.env.DryRun {
			lines = append(lines, "", dryRunNote())
		}
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
