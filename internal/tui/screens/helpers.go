package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// duration 紧凑的时长表示：3天4时、5时12分、3分20秒
func duration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d天%d时", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%d时%d分", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%d分%d秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%.1f秒", d.Seconds())
}

// statusLine 状态面板里的一行：标签 + 彩色圆点 + 文本
func statusLine(label string, ok bool, text string) string {
	dot := theme.Fg(theme.Green).Render("●")
	style := theme.Fg(theme.Green)
	if !ok {
		dot, style = theme.Fg(theme.Amber).Render("●"), theme.Fg(theme.Amber)
	}
	return widget.KV(label, 12, dot+" "+style.Render(text))
}

// bullets 带圆点的说明列表，自动折行
func bullets(items []string, w int) string {
	var lines []string
	for _, it := range items {
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			theme.MutedStyle.Render("· "), theme.MutedStyle.Width(w-2).Render(it)))
	}
	return strings.Join(lines, "\n")
}
