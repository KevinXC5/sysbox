package screens

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// SelfUpdateID 首页上触发自升级时打开的页面
const SelfUpdateID = "self-update"

// sysInfo 首页底部的系统概况
type sysInfo struct {
	os          string
	free, total int64
}

type sysInfoMsg sysInfo

// Home 首页：左侧分组的工具列表，右侧当前工具的详情
type Home struct {
	tools      []Tool
	cursor     int
	info       sysInfo
	newVersion string
}

// NewHome 创建首页
func NewHome(tools []Tool) *Home { return &Home{tools: tools} }

// SetNewVersion 有可用新版本时由根模型设置
func (m *Home) SetNewVersion(v string) { m.newVersion = v }

func (m *Home) Init() tea.Cmd { return loadSysInfo }

// loadSysInfo 读取系统版本与数据盘空间
func loadSysInfo() tea.Msg {
	var info sysInfo
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		info.os = "macOS " + strings.TrimSpace(string(out))
	}
	// APFS 下用户数据在 Data 卷，取不到时退回根卷
	var st syscall.Statfs_t
	if syscall.Statfs("/System/Volumes/Data", &st) == nil || syscall.Statfs("/", &st) == nil {
		info.free = int64(st.Bavail) * int64(st.Bsize)
		info.total = int64(st.Blocks) * int64(st.Bsize)
	}
	return sysInfoMsg(info)
}

func (m *Home) Busy() bool { return false }

func (m *Home) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sysInfoMsg:
		m.info = sysInfo(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.cursor = (m.cursor - 1 + len(m.tools)) % len(m.tools)
		case "down", "j":
			m.cursor = (m.cursor + 1) % len(m.tools)
		case "q", "esc":
			return tea.Quit
		case "u":
			if m.newVersion != "" {
				return Open(SelfUpdateID)
			}
		case "enter", " ":
			return Open(m.tools[m.cursor].ID)
		}
	}
	return nil
}

func (m *Home) Crumbs() []string { return nil }

func (m *Home) Hints() []string {
	hints := []string{"↑↓", "选择", "enter", "打开"}
	if m.newVersion != "" {
		hints = append(hints, "u", "升级")
	}
	return append(hints, "t", "主题", "q", "退出")
}

func (m *Home) Status() string {
	return theme.MutedStyle.Render(strconv.Itoa(len(m.tools)) + " 个工具")
}

func (m *Home) Body(w, h int) string {
	iw := w - 4
	// 品牌区：空间充足时显示大字 Logo，否则收成单行字标，再不够就省略
	tagline := theme.SubtleStyle.Render("个人 macOS 维护工具箱") + theme.MutedStyle.Render("   清理 · 进程治理 · 工具")
	var brand string
	switch {
	case h >= 30 && iw >= 56:
		brand = lipgloss.JoinVertical(lipgloss.Center, theme.Logo(), "", tagline)
	case h >= 16:
		brand = theme.BrandGradient("◆ SYSBOX") + "   " + tagline
	}
	blocks := []string{""}
	used := 3 // 顶部空行、底部系统信息及其上空行
	if brand != "" {
		brand = lipgloss.PlaceHorizontal(iw, lipgloss.Center, brand)
		blocks = append(blocks, brand, "")
		used += lipgloss.Height(brand) + 1
	}
	mainH := max(3, h-used)
	main := m.menu(iw, mainH)
	if iw >= 90 {
		lw := max(46, iw*45/100)
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.menu(lw, mainH), "  ", m.detail(iw-lw-2, mainH))
	}
	blocks = append(blocks, main, "", m.infoLine(iw))
	return widget.Inset(strings.Join(blocks, "\n"))
}

// menu 分组的工具列表，行数不够时去掉组间空行，仍不够则跟随光标滚动
func (m *Home) menu(w, h int) string {
	build := func(gap bool) ([]string, int) {
		var lines []string
		cur, group := 0, ""
		for i, t := range m.tools {
			if t.Group != group {
				group = t.Group
				if gap && len(lines) > 0 {
					lines = append(lines, "")
				}
				title := theme.Fg(theme.Accent2).Bold(true).Render(group)
				lines = append(lines, title+" "+theme.Rule(w-lipgloss.Width(title)-1))
			}
			if i == m.cursor {
				cur = len(lines)
			}
			lines = append(lines, m.row(t, i == m.cursor, w))
		}
		return lines, cur
	}
	lines, cur := build(true)
	if len(lines) > h {
		lines, cur = build(false)
	}
	off := widget.Scroll(cur, 0, len(lines), h)
	lines = lines[off:min(len(lines), off+h)]
	return lipgloss.NewStyle().Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// row 一个工具入口，选中行用左侧色条和品牌色标出；简介列随宽度伸缩
func (m *Home) row(t Tool, sel bool, w int) string {
	bar, nameStyle, descStyle := "  ", theme.TextStyle, theme.MutedStyle
	if sel {
		bar = theme.Fg(theme.Accent).Render("▌ ")
		nameStyle = theme.Fg(theme.Accent).Bold(true)
		descStyle = theme.SubtleStyle
	}
	nameW := min(22, max(12, (w-2)*2/5))
	return bar + widget.Cell(t.Name, nameW, nameStyle, false) + widget.Cell(t.Desc, w-2-nameW, descStyle, false)
}

// detail 右侧详情面板，介绍当前选中的工具
func (m *Home) detail(w, h int) string {
	t := m.tools[m.cursor]
	inner := widget.PanelInner(w)
	lines := []string{
		theme.BoldStyle.Render(t.Name),
		theme.MutedStyle.Render(t.Group + " · " + t.Desc),
		"",
		widget.Section("功能"),
	}
	for _, d := range t.Detail {
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			theme.Fg(theme.Accent).Render("· "), theme.TextStyle.Width(inner-2).Render(d)))
	}
	if t.Origin != "" {
		lines = append(lines, "", widget.Section("来源脚本"), theme.SubtleStyle.Width(inner).Render(t.Origin))
	}
	lines = append(lines, "", theme.Key("enter")+" "+theme.Fg(theme.Accent).Render("打开"))
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

// infoLine 系统概况：左侧系统版本，右侧数据盘占用条
func (m *Home) infoLine(w int) string {
	i := m.info
	if i.total == 0 {
		return theme.MutedStyle.Render("正在读取系统信息…")
	}
	barW := min(24, max(8, w/8))
	used := int(float64(i.total-i.free) / float64(i.total) * float64(barW))
	bar := theme.BrandGradient(strings.Repeat("━", used)) + theme.FaintStyle.Render(strings.Repeat("━", barW-used))
	right := theme.MutedStyle.Render("数据盘 ") + bar + " " +
		theme.SubtleStyle.Render("可用 "+fsx.FormatBytes(i.free)+" / "+fsx.FormatBytes(i.total))
	left := theme.SubtleStyle.Render(i.os)
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		return right
	}
	return widget.Spread(w, left, right)
}
