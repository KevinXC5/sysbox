// Package widget 提供各页面共用的界面组件：顶栏、底栏、面板、弹窗、进度条、步骤日志等。
// 组件只负责渲染，不持有状态。
package widget

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// HeaderInfo 顶栏右侧的全局状态
type HeaderInfo struct {
	DryRun     bool
	NewVersion string // 有可用新版本时非空
}

// Header 顶栏：品牌标 + 面包屑，右侧依次是新版本提示、演练标记、主题模式、版本号。
// 窗口变窄时从右往左依次省略。
func Header(w int, crumbs []string, info HeaderInfo) string {
	brand := lipgloss.NewStyle().Foreground(theme.OnColor).Background(theme.Accent).Bold(true).Padding(0, 1).Render("◆ sysbox")
	var parts []string
	for i, c := range crumbs {
		if i == len(crumbs)-1 {
			parts = append(parts, theme.BoldStyle.Render(c))
		} else {
			parts = append(parts, theme.SubtleStyle.Render(c))
		}
	}
	left := " " + brand
	if len(parts) > 0 {
		left += "  " + strings.Join(parts, theme.MutedStyle.Render("  ›  "))
	}

	var right []string
	if info.NewVersion != "" {
		right = append(right, theme.Fg(theme.Green).Bold(true).Render("↑ "+info.NewVersion+" 可升级"))
	}
	if info.DryRun {
		right = append(right, theme.Badge("演练模式", theme.Amber))
	}
	right = append(right,
		theme.MutedStyle.Render("◐ "+theme.CurrentMode().Label()),
		theme.MutedStyle.Render(meta.Version))
	for len(right) > 0 {
		r := strings.Join(right, "  ") + " "
		if lipgloss.Width(left)+lipgloss.Width(r)+2 <= w {
			return Spread(w, left, r) + "\n" + theme.Rule(w)
		}
		right = right[:len(right)-1]
	}
	return theme.Truncate(left, w) + "\n" + theme.Rule(w)
}

// Footer 底栏：分割线 + 左侧按键提示 + 右侧状态。宽度不足时优先保留状态，提示从后往前省略
func Footer(w int, hints []string, status string) string {
	sw := lipgloss.Width(status)
	if sw > w/2 {
		status, sw = "", 0
	}
	left := theme.HintsFit(w-sw-4, hints...)
	return theme.Rule(w) + "\n" + Spread(w, " "+left, status+" ")
}

// Frame 组装整屏：顶栏、主体、底栏，主体按剩余高度补齐或裁剪
func Frame(w, h int, head, body, foot string) string {
	bodyH := max(1, h-lipgloss.Height(head)-lipgloss.Height(foot))
	body = lipgloss.NewStyle().Width(w).Height(bodyH).MaxHeight(bodyH).MaxWidth(w).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
}

// Spread 左右两端对齐，中间用空格填满
func Spread(w int, left, right string) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return theme.Truncate(left+" "+right, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// Cell 固定宽度的单元格：超长截断，不足补齐，可右对齐
func Cell(text string, w int, style lipgloss.Style, right bool) string {
	if w <= 0 {
		return ""
	}
	t := ansi.Truncate(text, w, "…")
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(t)))
	if right {
		return pad + style.Render(t)
	}
	return style.Render(t) + pad
}

// Center 把内容放在给定区域正中
func Center(w, h int, s string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// Inset 页面主体的标准左右边距
func Inset(s string) string { return lipgloss.NewStyle().PaddingLeft(2).Render(s) }

// Panel 圆角边框面板，按给定的外框尺寸填满
func Panel(w, h int, border lipgloss.TerminalColor, content string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(1, 2).Width(max(1, w-2)).Height(max(1, h-2)).MaxHeight(h).Render(content)
}

// PanelInner 面板内容区的宽度
func PanelInner(w int) int { return w - 6 }

// Section 面板或页面里的小标题
func Section(title string) string { return theme.MutedStyle.Render(title) }

// KV 一行“标签  值”，标签定宽对齐
func KV(label string, labelW int, value string) string {
	return Cell(label, labelW, theme.MutedStyle, false) + value
}

// WrapPath 按路径分隔符折行，避免在目录名中间断开；单段超长时交给样式硬折行
func WrapPath(p string, w int) string {
	var lines []string
	cur := ""
	for _, seg := range strings.SplitAfter(p, "/") {
		if cur != "" && lipgloss.Width(cur+seg) > w {
			lines = append(lines, cur)
			cur = ""
		}
		cur += seg
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// Scroll 让光标保持在可视范围内，返回新的滚动偏移
func Scroll(cursor, offset, n, h int) int {
	if h <= 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+h {
		offset = cursor - h + 1
	}
	return max(0, min(offset, n-h))
}
