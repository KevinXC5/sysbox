package screens

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/obsidianlink"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// Obsidian 目录链接页：勾选一级子目录 → 预览变更 → 确认应用

type obsidianState int

const (
	obsUnconfigured obsidianState = iota
	obsError
	obsList
	obsConfirm
	obsDone
)

type obsidianPage struct {
	env    Env
	opts   obsidianlink.Options
	state  obsidianState
	err    error
	snap   obsidianlink.Snapshot
	chosen map[string]bool
	cursor int
	offset int

	focusOK bool
	steps   []sysx.Step
}

// ObsidianOptions 由配置生成工具参数，勾选记录放在 sysbox 配置目录下
func ObsidianOptions(c config.Obsidian) (obsidianlink.Options, bool) {
	if c.Src == "" || c.Dest == "" {
		return obsidianlink.Options{}, false
	}
	dir, _ := config.Dir()
	return obsidianlink.Options{
		Src:             expandHome(c.Src),
		Dest:            expandHome(c.Dest),
		DefaultExcludes: c.Excludes,
		StateFile:       filepath.Join(dir, "obsidian-link.selected"),
	}, true
}

// expandHome 展开配置里以 ~/ 开头的路径
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(fsx.Home(), p[2:])
	}
	return p
}

// NewObsidian 创建 Obsidian 目录链接页
func NewObsidian(env Env) Page {
	m := &obsidianPage{env: env}
	opts, ok := ObsidianOptions(env.Config.Obsidian)
	if !ok {
		m.state = obsUnconfigured
		return m
	}
	m.opts = opts
	m.reload()
	return m
}

func (m *obsidianPage) reload() {
	snap, err := obsidianlink.Load(m.opts)
	if err != nil {
		m.state, m.err = obsError, err
		return
	}
	m.snap, m.state = snap, obsList
	m.chosen = map[string]bool{}
	for n, v := range snap.Checked {
		m.chosen[n] = v
	}
}

func (m *obsidianPage) Init() tea.Cmd { return nil }
func (m *obsidianPage) Busy() bool    { return false }

func (m *obsidianPage) plan() obsidianlink.Plan {
	return obsidianlink.BuildPlan(m.opts, m.snap, m.chosen)
}

// chosenList 按源目录顺序排列的已勾选目录
func (m *obsidianPage) chosenList() []string {
	var out []string
	for _, n := range m.snap.Dirs {
		if m.chosen[n] {
			out = append(out, n)
		}
	}
	return out
}

func (m *obsidianPage) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	k := key.String()
	switch m.state {
	case obsList:
		switch k {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(m.snap.Dirs)-1, m.cursor+1)
		case " ":
			n := m.snap.Dirs[m.cursor]
			m.chosen[n] = !m.chosen[n]
		case "a":
			for _, n := range m.snap.Dirs {
				m.chosen[n] = true
			}
		case "n":
			m.chosen = map[string]bool{}
		case "enter":
			m.state, m.focusOK = obsConfirm, false
		case "esc", "q":
			return Back
		}
	case obsConfirm:
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.focusOK = !m.focusOK
		case "enter", "y":
			if k == "y" || m.focusOK {
				m.apply()
			} else {
				m.state = obsList
			}
		case "esc", "n", "q":
			m.state = obsList
		}
	case obsDone:
		switch k {
		case "r":
			m.reload()
		case "enter", "esc", "q":
			return Back
		}
	default:
		if k == "enter" || k == "esc" || k == "q" {
			return Back
		}
	}
	return nil
}

// apply 执行变更；演练模式只展示计划，不改链接也不写记录
func (m *obsidianPage) apply() {
	p := m.plan()
	if m.env.DryRun {
		rec := &sysx.Recorder{}
		for _, n := range p.Add {
			rec.Note("将链接 "+n, nil)
		}
		for _, n := range p.Remove {
			rec.Note("将移除 "+n, nil)
		}
		if p.Empty() {
			rec.Note("没有需要改动的链接", nil)
		}
		m.steps = rec.Steps
	} else {
		m.steps = obsidianlink.Apply(m.opts, m.snap, p, m.chosenList())
	}
	m.state = obsDone
}

// ---------- 渲染 ----------

func (m *obsidianPage) Crumbs() []string { return []string{"工具", "Obsidian 目录链接"} }

func (m *obsidianPage) Hints() []string {
	switch m.state {
	case obsList:
		return []string{"↑↓", "移动", "space", "勾选", "a", "全选", "n", "全不选", "enter", "应用", "esc", "返回", "t", "主题"}
	case obsConfirm:
		return []string{"←→", "切换", "enter", "确定", "esc", "取消"}
	case obsDone:
		return []string{"enter", "返回首页", "r", "重新读取"}
	}
	return []string{"enter", "返回首页"}
}

func (m *obsidianPage) Status() string {
	if m.state != obsList && m.state != obsConfirm {
		return ""
	}
	p := m.plan()
	return theme.Fg(theme.Accent).Render(fmt.Sprintf("已选 %d / %d", len(m.chosenList()), len(m.snap.Dirs))) +
		theme.MutedStyle.Render(fmt.Sprintf("  ·  新增 %d  移除 %d", len(p.Add), len(p.Remove)))
}

func (m *obsidianPage) Body(w, h int) string {
	switch m.state {
	case obsUnconfigured:
		return widget.Center(w, h, m.viewGuide(w))
	case obsError:
		return widget.Center(w, h, errorBox(w, "无法读取目录", m.err.Error()))
	case obsDone:
		return m.viewDone(w, h)
	}
	body := m.viewMain(w, h)
	if m.state == obsConfirm {
		return widget.Overlay(body, m.viewConfirm(w), w, h)
	}
	return body
}

func (m *obsidianPage) viewGuide(w int) string {
	dw := widget.DialogWidth(72, w)
	path, _ := config.Path()
	example := theme.SubtleStyle.Render(`{
  "obsidian": {
    "src": "~/Documents/工作",
    "dest": "~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Vault/工作",
    "excludes": ["归档", "素材"]
  }
}`)
	return widget.Dialog(theme.Accent, dw, strings.Join([]string{
		theme.BoldStyle.Render("先配置要链接的目录"),
		"",
		theme.SubtleStyle.Width(widget.DialogInner(dw)).Render("在配置文件中填写源目录、库内目标，以及首次运行时默认不勾选的子目录："),
		"",
		theme.MutedStyle.Render(fsx.PrettyPath(path)),
		"",
		example,
	}, "\n"))
}

func (m *obsidianPage) viewMain(w, h int) string {
	iw := w - 4
	info := m.viewInfo(iw)
	listH := max(3, h-lipgloss.Height(info)-3)
	var main string
	if iw >= 90 {
		lw := iw * 55 / 100
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.viewList(lw, listH), "  ", m.viewPlan(iw-lw-2, listH))
	} else {
		main = m.viewList(iw, listH)
	}
	return "\n" + widget.Inset(info+"\n\n"+main)
}

func (m *obsidianPage) viewInfo(iw int) string {
	state := map[obsidianlink.State]string{
		obsidianlink.StateMissing:   "目标不存在，将新建后再链接子目录",
		obsidianlink.StateFullLink:  "当前是整目录链接，应用后改为按子目录链接",
		obsidianlink.StateSelective: fmt.Sprintf("已按子目录链接 %d 个", len(m.snap.Linked)),
	}[m.snap.State]
	lines := []string{
		widget.KV("源目录", 12, theme.TextStyle.Render(theme.Truncate(fsx.PrettyPath(m.opts.Src), iw-12))),
		widget.KV("库内目标", 12, theme.TextStyle.Render(theme.Truncate(fsx.PrettyPath(m.opts.Dest), iw-12))),
		statusLine("当前", m.snap.State == obsidianlink.StateSelective, state),
	}
	if len(m.snap.Vanished) > 0 {
		lines = append(lines, widget.KV("已消失", 12, theme.MutedStyle.Render(strings.Join(m.snap.Vanished, "、"))))
	}
	return strings.Join(lines, "\n")
}

func (m *obsidianPage) viewList(w, h int) string {
	inner := widget.PanelInner(w)
	rowsH := h - 5
	m.offset = widget.Scroll(m.cursor, m.offset, len(m.snap.Dirs), rowsH)
	lines := []string{widget.Section("选择要链接进 Obsidian 的一级目录"), ""}
	for i := m.offset; i < min(len(m.snap.Dirs), m.offset+rowsH); i++ {
		n := m.snap.Dirs[i]
		mark, style := "  ", theme.TextStyle
		if i == m.cursor {
			mark, style = theme.Fg(theme.Accent).Render("▌ "), theme.Fg(theme.Accent).Bold(true)
		}
		check := theme.MutedStyle.Render("○ ")
		if m.chosen[n] {
			check = theme.Fg(theme.Green).Render("◉ ")
		} else if i != m.cursor {
			style = theme.SubtleStyle
		}
		tag := ""
		if m.snap.Linked[n] {
			tag = theme.MutedStyle.Render("已链接")
		}
		lines = append(lines, mark+check+widget.Cell(n, inner-4-8, style, false)+tag)
	}
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

// planLines 变更计划的文字预览
func (m *obsidianPage) planLines(w int) []string {
	p := m.plan()
	var lines []string
	group := func(title string, names []string, c lipgloss.TerminalColor, sign string) {
		if len(names) == 0 {
			return
		}
		lines = append(lines, theme.Fg(c).Bold(true).Render(fmt.Sprintf("%s  %d 个", title, len(names))))
		for _, n := range names {
			lines = append(lines, theme.Truncate("  "+theme.Fg(c).Render(sign+" ")+theme.SubtleStyle.Render(n), w))
		}
		lines = append(lines, "")
	}
	group("新增链接", p.Add, theme.Green, "+")
	group("移除链接", p.Remove, theme.Rose, "−")
	group("跳过（库内是真实路径，不会删除）", p.Skip, theme.Amber, "·")
	if len(p.Keep) > 0 {
		lines = append(lines, theme.MutedStyle.Render(fmt.Sprintf("保持不变  %d 个", len(p.Keep))))
	}
	if p.Empty() && m.snap.State == obsidianlink.StateSelective {
		lines = append(lines, theme.Fg(theme.Green).Render("✓ 没有需要改动的链接"))
	}
	if len(m.chosenList()) == 0 {
		lines = append(lines, theme.Fg(theme.Amber).Render("未勾选任何目录，库内将只保留空文件夹"))
	}
	return lines
}

func (m *obsidianPage) viewPlan(w, h int) string {
	lines := append([]string{widget.Section("变更预览"), ""}, m.planLines(widget.PanelInner(w))...)
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

func (m *obsidianPage) viewConfirm(bw int) string {
	w := widget.DialogWidth(64, bw)
	inner := widget.DialogInner(w)
	lines := m.planLines(inner)
	if len(lines) > 14 {
		lines = append(lines[:13], theme.MutedStyle.Render("…"))
	}
	parts := []string{theme.BoldStyle.Render("应用链接变更"), ""}
	parts = append(parts, lines...)
	if m.env.DryRun {
		parts = append(parts, "", dryRunNote())
	}
	parts = append(parts, "", widget.Buttons(inner,
		widget.Button("取消", !m.focusOK, theme.Subtle), widget.Button("应用", m.focusOK, theme.Accent)))
	return widget.Dialog(theme.Accent, w, strings.Join(parts, "\n"))
}

func (m *obsidianPage) viewDone(w, h int) string {
	iw := w - 4
	title := theme.Fg(theme.Green).Bold(true).Render("✓ ") + theme.BoldStyle.Render("链接已更新")
	if widget.StepsFailed(m.steps) {
		title = theme.Fg(theme.Amber).Bold(true).Render("! ") + theme.BoldStyle.Render("部分操作失败")
	}
	lines := []string{title, ""}
	if m.env.DryRun {
		lines = append(lines, dryRunNote(), "")
	}
	lines = append(lines, widget.StepLog(m.steps, widget.PanelInner(iw), h-10), "",
		theme.MutedStyle.Render("若 Obsidian 已打开，切换一次库或重启后，索引数量才会更新"))
	return "\n" + widget.Inset(widget.Panel(iw, h-2, theme.Faint, strings.Join(lines, "\n")))
}
