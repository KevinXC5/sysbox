package screens

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/disk"
	"github.com/KevinXC5/sysbox/internal/tools/network"
	"github.com/KevinXC5/sysbox/internal/tools/services"
)

func NewNetwork(env Env) Page {
	spec := systemSpec{name: "网络诊断", tabs: []string{"DNS", "TCP", "HTTP", "代理"}, defaults: []string{"example.com", "example.com:443", "https://example.com", "https://example.com"}, inputLabel: "诊断目标"}
	spec.load = func(ctx context.Context, tab int, target string) (systemResult, error) {
		results, err := network.Diagnose(ctx, tab, target)
		if err != nil {
			return systemResult{}, err
		}
		result := systemResult{note: "按 g 更换目标；TCP 使用主机:端口，HTTP 与代理使用网址"}
		for _, r := range results {
			result.rows = append(result.rows, systemRow{name: r.Title, summary: r.Summary, lines: r.Lines})
		}
		if tab == 3 {
			if lines, e := network.SystemProxy(ctx); e == nil {
				result.rows = append(result.rows, systemRow{name: "系统代理", summary: "仅展示配置", lines: strings.Split(lines, "\n")})
			} else {
				result.note += "；系统代理配置无法读取：" + e.Error()
			}
		}
		return result, nil
	}
	spec.actions = func(int, systemRow) []systemAction {
		return []systemAction{{key: "p", label: "进程与端口", tool: "processes"}}
	}
	return newSystemPage(env, spec)
}

func NewDisk(env Env) Page {
	home, err := os.UserHomeDir()
	if err != nil {
		return NewErrorPage([]string{"系统", "磁盘分析"}, err)
	}
	spec := systemSpec{name: "磁盘分析", tabs: []string{"目录占用", "大文件", "磁盘容量"}, defaults: []string{home, home, home}, inputLabel: "扫描目录", initialTab: 2, timeout: 2 * time.Minute}
	spec.parent = func(path string) string {
		expanded, e := disk.Expand(path)
		if e != nil {
			return path
		}
		return filepath.Dir(expanded)
	}
	spec.load = func(ctx context.Context, tab int, path string) (systemResult, error) {
		if tab == 2 {
			volumes, err := disk.Volumes(ctx)
			if err != nil {
				return systemResult{}, err
			}
			result := systemResult{note: "选择磁盘按 enter 扫描，或按 g 指定目录；大目录可按 esc 返回并取消扫描"}
			for _, v := range volumes {
				used := v.Total - v.Free
				pct := float64(used) / float64(max(int64(1), v.Total)) * 100
				result.rows = append(result.rows, systemRow{name: v.Path, summary: fmt.Sprintf("%.0f%% 已用", pct), target: v.Path, lines: []string{"设备 / 卷：" + v.Name, "挂载路径：" + v.Path, "总容量：" + fsx.FormatBytes(v.Total), "已用：" + fsx.FormatBytes(used), "可用：" + fsx.FormatBytes(v.Free)}, value: v})
			}
			return result, nil
		}
		report, err := disk.Scan(ctx, path)
		if err != nil {
			return systemResult{}, err
		}
		entries := report.Entries
		if tab == 1 {
			entries = report.Files
		}
		note := "扫描完成 · 合计 " + fsx.FormatBytes(report.Bytes) + " · 大文件最多显示 50 项"
		if runtime.GOOS == "windows" {
			note += " · 按文件逻辑大小统计"
		} else {
			note += " · 按分配块数统计，APFS 共享块可能重复计入"
		}
		if report.Skipped > 0 {
			note += fmt.Sprintf(" · %d 项无法读取，统计不完整", report.Skipped)
		}
		result := systemResult{note: note}
		for _, entry := range entries {
			kind := "文件"
			target := ""
			if entry.Directory {
				kind = "目录"
				target = entry.Path
			}
			lines := []string{"路径：" + entry.Path, "类型：" + kind, "占用：" + fsx.FormatBytes(entry.Bytes), "修改时间：" + entry.Modified.Format("2006-01-02 15:04:05"), "目录排行汇总所选目录下的全部内容，不跟随符号链接。"}
			if id := disk.CleanupTool(entry.Path); id != "" {
				lines = append(lines, "按 c 打开对应清理模块；该模块会按自身规则重新扫描。")
			}
			result.rows = append(result.rows, systemRow{name: filepath.Base(entry.Path), summary: fsx.FormatBytes(entry.Bytes), lines: lines, target: target, value: entry})
		}
		return result, nil
	}
	spec.actions = func(tab int, row systemRow) []systemAction {
		path := row.target
		if entry, ok := row.value.(disk.Entry); ok {
			path = entry.Path
			if !entry.Directory {
				path = filepath.Dir(path)
			}
		}
		if path == "" {
			return nil
		}
		command := sysx.C("open", path)
		if runtime.GOOS == "windows" {
			command = sysx.C("explorer.exe", path)
		}
		actions := []systemAction{{key: "o", label: "打开目录", run: func(ctx context.Context) (string, error) { return (sysx.ExecRunner{}).Run(ctx, command) }}}
		if entry, ok := row.value.(disk.Entry); ok {
			if id := disk.CleanupTool(entry.Path); id != "" {
				actions = append(actions, systemAction{key: "c", label: "清理入口", tool: id})
			}
		}
		return actions
	}
	return newSystemPage(env, spec)
}

func NewServices(env Env) Page { return newServicesPage(env, services.New()) }
func newServicesPage(env Env, client *services.Client) *systemPage {
	spec := systemSpec{name: "服务与启动项", tabs: []string{"服务", "启动项"}}
	spec.load = func(ctx context.Context, tab int, _ string) (systemResult, error) {
		snapshot, err := client.List(ctx, tab == 1)
		if err != nil {
			return systemResult{}, err
		}
		result := systemResult{note: snapshot.Warning}
		for _, item := range snapshot.Items {
			lines := []string{"名称：" + item.Name, "标识：" + item.ID, "范围：" + item.Scope, "状态：" + item.State, "启动方式：" + item.StartMode, fmt.Sprintf("进程 PID：%d", item.PID)}
			if item.Command != "" {
				lines = append(lines, "命令："+item.Command)
			}
			if item.Path != "" {
				lines = append(lines, "配置 / 位置："+item.Path)
			}
			if item.Startup && item.Domain == "" && runtime.GOOS == "windows" {
				lines = append(lines, "启动文件夹中的项目仅展示；可在系统启动应用设置中管理。")
			}
			for _, action := range services.Actions(item) {
				if action.Note != "" {
					lines = append(lines, "\n"+action.Label+"："+action.Note)
				}
			}
			result.rows = append(result.rows, systemRow{name: item.Name, summary: item.State, lines: lines, value: item})
		}
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
			actions = append(actions, systemAction{key: action.Key, label: action.Label, note: action.Note, preview: action.Command.String(), mutates: action.Mutates, run: func(ctx context.Context) (string, error) { return client.Execute(ctx, action, env.DryRun) }})
		}
		return actions
	}
	return newSystemPage(env, spec)
}
