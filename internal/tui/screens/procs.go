package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// procTable 进程表格：PID、CPU、内存、运行时长、累计 CPU、名称；flag 可为每行追加标记
func procTable(procs []sysx.Proc, w, maxRows int, flag func(sysx.Proc) string) string {
	head := theme.MutedStyle.Render(
		widget.Cell("PID", 8, theme.MutedStyle, false) + widget.Cell("CPU", 8, theme.MutedStyle, true) +
			widget.Cell("内存", 10, theme.MutedStyle, true) + "  " + widget.Cell("运行时长", 12, theme.MutedStyle, false) +
			widget.Cell("累计 CPU", 12, theme.MutedStyle, false) + "名称")
	rows := []string{theme.Truncate(head, w)}
	for i, p := range procs {
		if i >= maxRows {
			rows = append(rows, theme.MutedStyle.Render(fmt.Sprintf("… 另外 %d 个进程", len(procs)-maxRows)))
			break
		}
		cpuStyle := theme.TextStyle
		if p.CPU >= 50 {
			cpuStyle = theme.Fg(theme.Rose).Bold(true)
		}
		row := widget.Cell(fmt.Sprint(p.PID), 8, theme.SubtleStyle, false) +
			widget.Cell(fmt.Sprintf("%.1f%%", p.CPU), 8, cpuStyle, true) +
			widget.Cell(fsx.FormatBytes(p.RSS), 10, theme.SubtleStyle, true) + "  " +
			widget.Cell(duration(p.Elapsed), 12, theme.SubtleStyle, false) +
			widget.Cell(duration(p.CPUTime), 12, theme.SubtleStyle, false) +
			theme.TextStyle.Render(p.Name())
		if flag != nil {
			row += "  " + flag(p)
		}
		rows = append(rows, theme.Truncate(row, w))
	}
	return strings.Join(rows, "\n")
}

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
