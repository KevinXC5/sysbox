package widget

import (
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// Dialog 通用对话框外壳，w 为含边框的总宽度
func Dialog(border lipgloss.TerminalColor, w int, content string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(1, 3).Width(w - 2).Render(content)
}

// DialogInner 对话框内容区宽度
func DialogInner(w int) int { return w - 8 }

// DialogWidth 按窗口宽度限制对话框宽度
func DialogWidth(want, bodyW int) int { return min(want, bodyW-4) }

// Button 对话框按钮，聚焦时填充底色
func Button(label string, focused bool, c lipgloss.TerminalColor) string {
	if focused {
		return lipgloss.NewStyle().Foreground(theme.OnColor).Background(c).Bold(true).Padding(0, 3).Render(label)
	}
	return lipgloss.NewStyle().Foreground(theme.Subtle).Background(theme.Surface).Padding(0, 3).Render(label)
}

// Buttons 右对齐的一组按钮
func Buttons(inner int, buttons ...string) string {
	return lipgloss.NewStyle().Width(inner).Align(lipgloss.Right).Render(strings.Join(buttons, "  "))
}

// Overlay 把弹窗叠加在压暗的背景画面正中。
// 背景去掉原有样式后统一用分割线色重绘，形成遮罩效果，弹窗之外的内容仍隐约可见。
func Overlay(bg, fg string, w, h int) string {
	bg = lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).MaxWidth(w).Render(bg)
	lines := strings.Split(bg, "\n")
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	fgLines := strings.Split(fg, "\n")
	fw := lipgloss.Width(fg)
	x, y := max(0, (w-fw)/2), max(0, (h-len(fgLines))/2)

	out := make([]string, len(lines))
	for r, plain := range lines {
		i := r - y
		if i < 0 || i >= len(fgLines) {
			out[r] = theme.FaintStyle.Render(plain)
			continue
		}
		// 宽字符可能被从中间切开，左右两段按目标宽度补齐
		left := ansi.Cut(plain, 0, x)
		left += strings.Repeat(" ", max(0, x-lipgloss.Width(left)))
		mid := fgLines[i] + strings.Repeat(" ", max(0, fw-lipgloss.Width(fgLines[i])))
		right := ansi.Cut(plain, x+fw, w)
		right = strings.Repeat(" ", max(0, w-x-fw-lipgloss.Width(right))) + right
		out[r] = theme.FaintStyle.Render(left) + mid + theme.FaintStyle.Render(right)
	}
	return strings.Join(out, "\n")
}

var barCache struct {
	sync.Mutex
	key   [3]string
	model progress.Model
	ready bool
}

// Bar reuses the gradient model while updating width and theme colors.
func Bar(w int, pct float64) string {
	key := [3]string{theme.Hex(theme.Accent), theme.Hex(theme.Accent2), theme.Hex(theme.Faint)}
	barCache.Lock()
	defer barCache.Unlock()
	if !barCache.ready || barCache.key != key {
		barCache.model = progress.New(progress.WithGradient(key[0], key[1]),
			progress.WithoutPercentage(), progress.WithFillCharacters('━', '━'))
		barCache.model.EmptyColor = key[2]
		barCache.key, barCache.ready = key, true
	}
	barCache.model.Width = max(1, w)
	return barCache.model.ViewAs(max(0, min(1, pct)))
}

// ProgressHead 进度类页面的标题行与满宽进度条
func ProgressHead(w int, title, right string, pct float64) string {
	return strings.Join([]string{"", Spread(w, title, right), "", Bar(w, pct), ""}, "\n")
}
