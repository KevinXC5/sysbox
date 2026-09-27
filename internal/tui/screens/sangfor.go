package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/sangfor"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// sangforTool 深信服客户端启停
type sangforTool struct{}

// NewSangfor 创建深信服页面
func NewSangfor(env Env) Page { return NewOps[sangfor.Status](sangforTool{}, env) }

func (sangforTool) Crumbs() []string        { return []string{"进程治理", "深信服客户端"} }
func (sangforTool) Interval() time.Duration { return 3 * time.Second }

func client(r sysx.Runner) *sangfor.Client {
	c := sangfor.New()
	c.Runner = r
	return c
}

func (sangforTool) Load(ctx context.Context, r sysx.Runner) (sangfor.Status, error) {
	return client(r).Status(ctx)
}

func (sangforTool) Render(s sangfor.Status, w, h int) string {
	if len(s.Services) == 0 {
		return widget.Section("深信服客户端") + "\n\n" + theme.MutedStyle.Render("没有找到 com.sangfor.* 服务，可能未安装客户端")
	}
	run := statusLine("客户端", true, "已全部停止")
	if s.Running() {
		run = statusLine("客户端", false, fmt.Sprintf("运行中，%d 个进程", len(s.Procs)))
	}
	autostart := 0
	for _, svc := range s.Services {
		if !svc.Disabled {
			autostart++
		}
	}
	boot := statusLine("开机自启", autostart == 0, "已禁用")
	if autostart > 0 {
		boot = statusLine("开机自启", false, fmt.Sprintf("%d 个服务会自启", autostart))
	}
	eco := statusLine("ecosystemd", true, "未运行")
	if len(s.Eco) > 0 {
		cpu := s.Eco[0].CPU
		eco = statusLine("ecosystemd", cpu < 30, fmt.Sprintf("CPU %.1f%%", cpu))
		if cpu >= 30 {
			eco = widget.KV("ecosystemd", 12, theme.Fg(theme.Rose).Bold(true).Render(fmt.Sprintf("● CPU %.1f%%，占用偏高", cpu)))
		}
	}

	lines := []string{widget.Section("深信服客户端状态"), "", run, boot, eco, "", widget.Section("launchd 服务")}
	nameW := max(20, w-30)
	for _, svc := range s.Services {
		domain := "用户"
		if svc.System {
			domain = "系统"
		}
		loaded := theme.MutedStyle.Render("未加载")
		if svc.Loaded {
			loaded = theme.Fg(theme.Green).Render("已加载")
		}
		auto := theme.Fg(theme.Amber).Render("自启")
		if svc.Disabled {
			auto = theme.MutedStyle.Render("已禁用")
		}
		label := strings.TrimPrefix(svc.Label, "com.sangfor.")
		lines = append(lines, theme.Truncate(widget.Cell(label, nameW, theme.TextStyle, false)+
			widget.Cell(domain, 6, theme.MutedStyle, false)+widget.Cell(loaded, 10, theme.TextStyle, false)+auto, w))
	}
	if s.Running() {
		lines = append(lines, "", widget.Section("相关进程"), procTable(s.Procs, w, max(3, h-len(lines)-4), nil))
	}
	return strings.Join(lines, "\n")
}

func (sangforTool) Actions(s sangfor.Status) []Action {
	if len(s.Services) == 0 {
		return nil
	}
	return []Action{
		{
			Key: "s", Label: "停止并禁用", Danger: true, Sudo: true,
			Desc:    "卸载全部服务、结束进程、重启 ecosystemd",
			Confirm: "卸载并永久禁用全部深信服 launchd 服务（重启后不再自启），结束残留进程，最后重启 ecosystemd 清除高 CPU 状态。",
			Run: func(ctx context.Context, r sysx.Runner) []sysx.Step {
				return client(r).Stop(ctx)
			},
		},
		{
			Key: "a", Label: "启动并恢复", Sudo: true,
			Desc:    "解除禁用并按依赖顺序加载全部服务",
			Confirm: "按依赖顺序解除禁用并加载全部深信服服务，恢复开机自启。若 ecosystemd 再次飙高，说明客户端版本与当前系统仍不兼容。",
			Run: func(ctx context.Context, r sysx.Runner) []sysx.Step {
				return client(r).Start(ctx)
			},
		},
	}
}
