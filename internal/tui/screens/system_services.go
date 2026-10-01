package screens

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/tools/services"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

var serviceViews = []string{"第三方", "运行中", "异常", "已禁用", "全部"}

func NewServices(env Env) Page { return newServicesPage(env, services.New()) }

func newServicesPage(env Env, client *services.Client) *systemPage {
	spec := systemSpec{name: "服务与启动项", tabs: []string{"服务", "启动项"}, views: serviceViews}
	spec.columns = func(int) []systemColumn {
		return []systemColumn{{title: "名称"}, {title: "状态", width: 12}, {title: "来源", width: 6}, {title: "启动方式", width: 12}, {title: "PID", width: 7, right: true}}
	}
	// 默认只看第三方：系统自带的服务数量多且通常无需处理
	spec.view = func(view int, row systemRow) bool {
		item, ok := row.value.(services.Item)
		if !ok {
			return true
		}
		switch view {
		case 0:
			return item.Source == services.SourceThirdParty
		case 1:
			return item.Running
		case 2:
			return item.Failed
		case 3:
			return item.Disabled
		}
		return true
	}
	spec.load = func(ctx context.Context, tab int, _ string, _ bool) (systemResult, error) {
		snapshot, err := client.List(ctx, tab == 1)
		if err != nil {
			return systemResult{}, err
		}
		counts := map[string]int{}
		result := systemResult{warn: snapshot.Warning}
		for _, item := range snapshot.Items {
			counts[item.Source]++
			if item.Running {
				counts["运行中"]++
			}
			if item.Failed {
				counts["异常"]++
			}
			result.rows = append(result.rows, serviceRow(item))
		}
		result.info = fmt.Sprintf("共 %d 项 · 第三方 %d · 运行中 %d · 异常 %d · 按 f 切换视图", len(snapshot.Items), counts[services.SourceThirdParty], counts["运行中"], counts["异常"])
		return result, nil
	}
	spec.actions = func(_ int, row systemRow) []systemAction {
		item, ok := row.value.(services.Item)
		if !ok {
			return nil
		}
		var actions []systemAction
		for _, action := range services.Actions(item) {
			action := action
			actions = append(actions, systemAction{key: action.Key, label: action.Label, note: action.Note, preview: action.Display(), mutates: action.Mutates, quiet: action.Key == "o", run: func(ctx context.Context) (string, error) { return client.Execute(ctx, action, env.DryRun) }})
		}
		return actions
	}
	return newSystemPage(env, spec)
}

func serviceRow(item services.Item) systemRow {
	tone := lipgloss.TerminalColor(theme.Subtle)
	switch {
	case item.Failed:
		tone = theme.Rose
	case item.Running:
		tone = theme.Green
	case item.Disabled:
		tone = theme.Muted
	}
	pid := "—"
	if item.PID > 0 {
		pid = fmt.Sprint(item.PID)
	}
	fields := []containers.Field{{Value: "基本信息"}, {Label: "名称", Value: item.Name}, {Label: "标识", Value: item.ID}, {Label: "来源", Value: item.Source}, {Label: "范围", Value: item.Scope}, {Label: "状态", Value: item.State}, {Label: "启动方式", Value: item.StartMode}, {Label: "PID", Value: pid}}
	if item.Source != services.SourceThirdParty && item.Kind != "service" {
		fields = append(fields, containers.Field{Label: "说明", Value: "系统自带或应用自动注册的项目，只提供查看与日志"})
	} else if item.Admin {
		fields = append(fields, containers.Field{Label: "权限", Value: "修改需要管理员授权，执行时会弹出系统授权窗口"})
	}
	if item.Command != "" || item.Path != "" {
		fields = append(fields, containers.Field{Value: "程序"}, containers.Field{Label: "命令", Value: item.Command}, containers.Field{Label: "配置 / 位置", Value: item.Path})
	}
	if len(item.LogPaths) > 0 {
		fields = append(fields, containers.Field{Value: "日志"}, containers.Field{Label: "文件", Value: strings.Join(item.LogPaths, "\n")})
	}
	return systemRow{name: item.Name, key: item.ID, cells: []string{item.State, item.Source, item.StartMode, pid}, tone: tone, ratio: -1, fields: fields, value: item}
}
