package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/cursorui"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// cursorTool CursorUIViewService 诊断
type cursorTool struct{}

// NewCursorUI 创建 CursorUIViewService 页面
func NewCursorUI(env Env) Page { return NewOps[cursorui.Report](cursorTool{}, env) }

func (cursorTool) Crumbs() []string        { return []string{"进程治理", "CursorUIViewService"} }
func (cursorTool) Interval() time.Duration { return 0 } // lsof 较重，只在打开、操作后和手动刷新时分析

func (cursorTool) Load(ctx context.Context, r sysx.Runner) (cursorui.Report, error) {
	return (&cursorui.Service{Runner: r}).Diagnose(ctx)
}

func (cursorTool) Render(rep cursorui.Report, w, h int) string {
	if len(rep.Procs) == 0 {
		return widget.Section("CursorUIViewService") + "\n\n" + statusLine("进程", true, "未运行，系统会在需要时自动拉起")
	}
	lines := []string{widget.Section("进程"), procTable(rep.Procs, w, 3, nil), "", widget.Section("重点判断")}
	for _, f := range rep.Findings {
		switch f.Level {
		case cursorui.LevelWarn:
			lines = append(lines, theme.Fg(theme.Rose).Render("⚠ "+f.Text))
		case cursorui.LevelOK:
			lines = append(lines, theme.Fg(theme.Green).Render("✓ "+f.Text))
		default:
			lines = append(lines, theme.Fg(theme.Amber).Render("· "+f.Text))
		}
	}
	lines = append(lines, "", widget.Section(fmt.Sprintf("涉及的第三方 App（共分析 %d 个打开的文件）", rep.Files)))
	if len(rep.Apps) == 0 {
		lines = append(lines, theme.MutedStyle.Render("没有第三方 App"))
	}
	for _, a := range rep.Apps {
		lines = append(lines, theme.Truncate(theme.SubtleStyle.Render(a), w))
	}
	if rep.SystemApps > 0 {
		lines = append(lines, theme.MutedStyle.Render(fmt.Sprintf("另有 %d 个系统自带组件，属正常加载", rep.SystemApps)))
	}
	if len(rep.Suspects) > 0 {
		lines = append(lines, "", widget.Section("可疑明细"))
		room := max(3, h-len(lines))
		for i, s := range rep.Suspects {
			if i >= room {
				lines = append(lines, theme.MutedStyle.Render(fmt.Sprintf("… 另外 %d 条", len(rep.Suspects)-room)))
				break
			}
			lines = append(lines, theme.Truncate(widget.Cell(s.Type, 6, theme.MutedStyle, false)+theme.SubtleStyle.Render(s.Name), w))
		}
	}
	return strings.Join(lines, "\n")
}

func (cursorTool) Actions(rep cursorui.Report) []Action {
	if len(rep.Procs) == 0 {
		return nil
	}
	procs := rep.Procs
	return []Action{{
		Key: "k", Label: "结束进程", Danger: true,
		Desc:    "先正常退出，超时再强制结束，系统会按需重新拉起",
		Confirm: fmt.Sprintf("结束 %d 个 CursorUIViewService 进程。先发送 TERM，2 秒内未退出再强制结束；系统会在需要时自动重新拉起。", len(procs)),
		Run: func(ctx context.Context, r sysx.Runner) []sysx.Step {
			res := (&cursorui.Service{Runner: r}).Kill(ctx, procs)
			rec := &sysx.Recorder{}
			if len(res.Alive) > 0 {
				rec.Note("结束进程", sysx.AliveError{PIDs: res.Alive})
			} else {
				rec.Note("已结束进程 "+sysx.JoinPIDs(sysx.PIDs(procs)), nil)
			}
			if len(res.Restarted) > 0 {
				rec.Note("系统已重新拉起新进程 "+sysx.JoinPIDs(sysx.PIDs(res.Restarted)), nil)
			}
			return rec.Steps
		},
	}}
}
