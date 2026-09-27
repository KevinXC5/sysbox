package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/appstore"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// appstoreTool appstoreagent 死循环治理
type appstoreTool struct{}

// NewAppStore 创建 appstoreagent 页面
func NewAppStore(env Env) Page { return NewOps[appstore.Status](appstoreTool{}, env) }

func (appstoreTool) Crumbs() []string        { return []string{"进程治理", "appstoreagent"} }
func (appstoreTool) Interval() time.Duration { return 3 * time.Second }

func (appstoreTool) Load(ctx context.Context, r sysx.Runner) (appstore.Status, error) {
	return (&appstore.Service{Runner: r}).Status(ctx)
}

func (appstoreTool) Render(s appstore.Status, w, h int) string {
	auto := statusLine("自动拉起", s.Disabled, "已禁用")
	if !s.Disabled {
		auto = statusLine("自动拉起", false, "未禁用（系统默认）")
	}
	looping := 0
	for _, p := range s.Procs {
		if appstore.Looping(p) {
			looping++
		}
	}
	proc := statusLine("进程", true, "未运行")
	switch {
	case looping > 0:
		proc = widget.KV("进程", 12, theme.Fg(theme.Rose).Bold(true).Render(fmt.Sprintf("● %d 个疑似死循环", looping)))
	case len(s.Procs) > 0:
		proc = statusLine("进程", true, fmt.Sprintf("运行中 %d 个，占用正常", len(s.Procs)))
	}
	lines := []string{widget.Section("appstoreagent 状态"), "", auto, proc}
	if len(s.Procs) > 0 {
		lines = append(lines, "", procTable(s.Procs, w, 5, func(p sysx.Proc) string {
			ratio := fmt.Sprintf("占比 %.0f%%", p.BusyRatio()*100)
			if appstore.Looping(p) {
				return theme.Fg(theme.Rose).Render(ratio + " 疑似死循环")
			}
			return theme.MutedStyle.Render(ratio)
		}))
	}
	lines = append(lines, "", widget.Section("说明"), bullets([]string{
		"部分 macOS 版本升级后，appstoreagent 会陷入 ArcadeManager 死循环并长期占满一个核",
		"累计 CPU 时间超过运行时长的 40% 即判定为疑似死循环",
		"禁用后只失去后台自动更新检查，App Store 的浏览、购买和手动更新不受影响",
		"launchctl disable 对按需拉起的服务抑制有限，进程仍可能被系统事件重新拉起",
	}, w))
	return strings.Join(lines, "\n")
}

func (appstoreTool) Actions(s appstore.Status) []Action {
	var acts []Action
	if !s.Disabled || len(s.Procs) > 0 {
		acts = append(acts, Action{
			Key: "d", Label: "禁用并结束", Danger: true,
			Desc:    "禁用自动拉起，并结束当前进程",
			Confirm: "写入持久化的禁用标记（重启后仍生效），卸载服务定义，并结束正在运行的 appstoreagent。",
			Run: func(ctx context.Context, r sysx.Runner) []sysx.Step {
				return (&appstore.Service{Runner: r}).Disable(ctx)
			},
		})
	}
	if s.Disabled {
		acts = append(acts, Action{
			Key: "e", Label: "恢复默认",
			Desc:    "清除禁用标记并重新拉起，Apple 修复后使用",
			Confirm: "清除禁用标记并重新加载 appstoreagent，恢复系统默认行为。",
			Run: func(ctx context.Context, r sysx.Runner) []sysx.Step {
				return (&appstore.Service{Runner: r}).Enable(ctx)
			},
		})
	}
	return acts
}
