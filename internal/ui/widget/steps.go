package widget

import (
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// StepLog 渲染操作步骤记录：成功 ✓、可接受的失败 ·、失败 ✗，命令文本弱化显示在下一行。
// 超出高度时只保留最后几条。
func StepLog(steps []sysx.Step, w, h int) string {
	var lines []string
	for _, s := range steps {
		icon, title := theme.Fg(theme.Green).Render("✓"), theme.TextStyle.Render(s.Title)
		switch {
		case s.Err != nil && s.Soft:
			icon, title = theme.MutedStyle.Render("·"), theme.SubtleStyle.Render(s.Title)
		case s.Err != nil:
			icon, title = theme.Fg(theme.Rose).Render("✗"), theme.Fg(theme.Rose).Render(s.Title)
		}
		line := icon + " " + title
		if s.Err != nil && !s.Soft {
			line += theme.MutedStyle.Render("  " + s.Err.Error())
		}
		lines = append(lines, theme.Truncate(line, w))
		if s.Cmd != "" {
			lines = append(lines, theme.Truncate("  "+theme.FaintStyle.Render("$ ")+theme.MutedStyle.Render(s.Cmd), w))
		}
	}
	if h > 0 && len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return strings.Join(lines, "\n")
}

// StepsFailed 是否存在不可接受的失败
func StepsFailed(steps []sysx.Step) bool {
	for _, s := range steps {
		if s.Err != nil && !s.Soft {
			return true
		}
	}
	return false
}
