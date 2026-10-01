package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func (c *Client) list(ctx context.Context, startup bool) (Snapshot, error) {
	userDomain := "gui/" + strconv.Itoa(os.Getuid())
	snapshot := Snapshot{}
	loaded := map[string]Item{}
	disabled := map[string]bool{}
	warnings := []string{}
	for _, domain := range []string{userDomain, "system"} {
		out, err := c.Runner.Run(ctx, sysx.C("launchctl", "print", domain))
		if err != nil {
			warnings = append(warnings, domain+" 状态读取失败："+err.Error())
		} else {
			for _, item := range decodeDomain(out, domain) {
				loaded[item.ID] = item
			}
		}
		out, err = c.Runner.Run(ctx, sysx.C("launchctl", "print-disabled", domain))
		if err != nil {
			warnings = append(warnings, domain+" 启用状态读取失败："+err.Error())
		} else {
			for name, value := range decodeDisabled(out) {
				disabled[domain+"/"+name] = value
			}
		}
	}
	if !startup {
		for id, item := range loaded {
			item.StartMode = "按需 / 由配置决定"
			if disabled[id] {
				item.StartMode = "已禁用"
			}
			snapshot.Items = append(snapshot.Items, item)
		}
		if len(loaded) == 0 {
			return snapshot, fmt.Errorf("无法读取 launchd 服务：%s", strings.Join(warnings, "；"))
		}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return snapshot, err
		}
		roots := []struct{ path, domain string }{{filepath.Join(home, "Library/LaunchAgents"), userDomain}, {"/Library/LaunchAgents", userDomain}, {"/Library/LaunchDaemons", "system"}}
		for _, root := range roots {
			entries, err := os.ReadDir(root.path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				warnings = append(warnings, root.path+" 无法读取")
				continue
			}
			for _, entry := range entries {
				if ctx.Err() != nil {
					return snapshot, ctx.Err()
				}
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plist") {
					continue
				}
				path := filepath.Join(root.path, entry.Name())
				out, err := c.Runner.Run(ctx, sysx.C("plutil", "-convert", "json", "-o", "-", "--", path))
				if err != nil {
					warnings = append(warnings, entry.Name()+" 解析失败")
					continue
				}
				item, err := decodePlist(out, path, root.domain)
				if err != nil {
					warnings = append(warnings, entry.Name()+" 解析失败")
					continue
				}
				if active, ok := loaded[item.ID]; ok {
					item.State = active.State
					item.PID = active.PID
				} else {
					item.State = "未加载"
				}
				if value, ok := disabled[item.ID]; ok {
					if value {
						item.StartMode = "已禁用"
					} else {
						item.StartMode = "已启用"
					}
				}
				snapshot.Items = append(snapshot.Items, item)
			}
		}
	}
	snapshot.Warning = strings.Join(warnings, "；")
	return snapshot, nil
}

// launchctl print 的 services 区域包含 PID、上次退出状态与服务标签。
func decodeDomain(out, domain string) []Item {
	inside := false
	var items []Item
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "services = {" {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if line == "}" {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		name := strings.Join(fields[2:], " ")
		state := "等待触发"
		if pid > 0 {
			state = "运行中"
		} else if fields[1] != "0" && fields[1] != "-" && fields[1] != "(pe)" {
			state = "已退出（" + fields[1] + "）"
		}
		scope := "用户"
		if domain == "system" {
			scope = "系统"
		}
		items = append(items, Item{ID: domain + "/" + name, Name: name, Domain: domain, Scope: scope, State: state, PID: pid})
	}
	return items
}
func decodeDisabled(out string) map[string]bool {
	values := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=>", 2)
		if len(parts) != 2 {
			continue
		}
		name, err := strconv.Unquote(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		value := strings.TrimSpace(parts[1])
		if value == "true" || value == "false" {
			values[name] = value == "true"
		}
	}
	return values
}
func decodePlist(out, path, domain string) (Item, error) {
	var data struct {
		Label, Program, StandardOutPath, StandardErrorPath string
		ProgramArguments                                   []string
		RunAtLoad, Disabled                                bool
		KeepAlive                                          any
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return Item{}, err
	}
	if data.Label == "" {
		return Item{}, fmt.Errorf("缺少 Label")
	}
	command := data.Program
	if len(data.ProgramArguments) > 0 {
		command = strings.Join(data.ProgramArguments, " ")
	}
	mode := "按需启动"
	if data.RunAtLoad {
		mode = "加载时启动"
	}
	if data.Disabled {
		mode = "已禁用（配置）"
	}
	scope := "用户"
	if domain == "system" {
		scope = "系统"
	}
	item := Item{ID: domain + "/" + data.Label, Name: data.Label, Scope: scope, Domain: domain, Command: command, Path: path, StartMode: mode, Startup: true}
	for _, p := range []string{data.StandardOutPath, data.StandardErrorPath} {
		if filepath.IsAbs(p) {
			item.LogPaths = append(item.LogPaths, p)
		}
	}
	return item, nil
}
func Actions(item Item) []Action {
	target := item.ID
	start := sysx.C("launchctl", "kickstart", target)
	if item.State == "未加载" && item.Path != "" {
		start = sysx.C("launchctl", "bootstrap", item.Domain, item.Path)
	}
	actions := []Action{
		{Key: "s", Label: "启动", Command: start, Mutates: true, Note: "启动所选服务；系统服务可能需要管理员权限"},
		{Key: "x", Label: "停止", Command: sysx.C("launchctl", "kill", "SIGTERM", target), Mutates: true, Note: "向服务发送 SIGTERM；KeepAlive 服务可能自动重启"},
		{Key: "e", Label: "启用", Command: sysx.C("launchctl", "enable", target), Mutates: true, Note: "允许后续加载；不会立即启动服务"},
		{Key: "d", Label: "禁用", Command: sysx.C("launchctl", "disable", target), Mutates: true, Note: "阻止后续加载；已经运行的服务不会立即停止"},
		{Key: "v", Label: "详情", Command: sysx.C("launchctl", "print", target)},
	}
	logs := sysx.C("/usr/bin/log", "show", "--last", "10m", "--style", "compact", "--predicate", "eventMessage CONTAINS[c] "+strconv.Quote(item.Name))
	if len(item.LogPaths) > 0 {
		logs = sysx.C("tail", append([]string{"-n", "200", "--"}, item.LogPaths...)...)
	}
	return append(actions, Action{Key: "l", Label: "日志", Command: logs, Note: "读取配置日志末尾 200 行；没有文件日志时查询近 10 分钟包含服务标签的系统日志"})
}
