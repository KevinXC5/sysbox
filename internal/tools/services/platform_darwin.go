package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// 更新器可能保留合法的空字典作为旧启动项的占位文件。
var errPlaceholderPlist = errors.New("空启动项占位文件")

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
	plists, err := c.plists(ctx, userDomain, &warnings)
	if err != nil {
		return snapshot, err
	}
	apply := func(item *Item) {
		if value, ok := disabled[item.ID]; ok {
			item.Disabled = value
		}
		if item.Disabled {
			item.StartMode = "已禁用"
		}
		classify(item)
	}
	if !startup {
		if len(loaded) == 0 {
			return snapshot, fmt.Errorf("无法读取 launchd 服务：%s", strings.Join(warnings, "；"))
		}
		for id, item := range loaded {
			item.StartMode = "按需启动"
			if p, ok := plists[id]; ok {
				item.Command, item.Path, item.LogPaths, item.StartMode, item.Disabled = p.Command, p.Path, p.LogPaths, p.StartMode, p.Disabled
			}
			apply(&item)
			snapshot.Items = append(snapshot.Items, item)
		}
	} else {
		for id, item := range plists {
			if active, ok := loaded[id]; ok {
				item.State, item.PID, item.Running, item.Failed = active.State, active.PID, active.Running, active.Failed
			} else {
				item.State = "未加载"
			}
			apply(&item)
			snapshot.Items = append(snapshot.Items, item)
		}
		logins, err := c.loginItems(ctx)
		if err != nil {
			warnings = append(warnings, "登录项读取失败（需允许终端控制“系统事件”）："+err.Error())
		}
		snapshot.Items = append(snapshot.Items, logins...)
	}
	snapshot.Warning = strings.Join(warnings, "；")
	return snapshot, nil
}

// plists 读取第三方 LaunchAgents 与 LaunchDaemons 配置，系统自带的位于 /System 下不在此列
func (c *Client) plists(ctx context.Context, userDomain string, warnings *[]string) (map[string]Item, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	items := map[string]Item{}
	roots := []struct{ path, domain string }{{filepath.Join(home, "Library/LaunchAgents"), userDomain}, {"/Library/LaunchAgents", userDomain}, {"/Library/LaunchDaemons", "system"}}
	for _, root := range roots {
		entries, err := os.ReadDir(root.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			*warnings = append(*warnings, root.path+" 无法读取")
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plist") {
				continue
			}
			path := filepath.Join(root.path, entry.Name())
			out, err := c.Runner.Run(ctx, sysx.C("plutil", "-convert", "json", "-o", "-", "--", path))
			if err != nil {
				*warnings = append(*warnings, entry.Name()+" 读取失败："+err.Error())
				continue
			}
			item, err := decodePlist(out, path, root.domain)
			if errors.Is(err, errPlaceholderPlist) {
				continue
			}
			if err != nil {
				*warnings = append(*warnings, entry.Name()+" 配置无效："+err.Error())
				continue
			}
			items[item.ID] = item
		}
	}
	return items, nil
}

// loginItems 读取系统设置中的“登录时打开”项目
func (c *Client) loginItems(ctx context.Context) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := c.Runner.Run(ctx, sysx.C("osascript", "-e", `set out to ""`, "-e", `tell application "System Events" to repeat with i in login items`, "-e", `set out to out & (name of i) & tab & (path of i) & linefeed`, "-e", "end repeat", "-e", "return out"))
	if err != nil {
		return nil, err
	}
	return decodeLoginItems(out), nil
}

func decodeLoginItems(out string) []Item {
	var items []Item
	for _, line := range strings.Split(out, "\n") {
		name, path, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if name == "" {
			continue
		}
		items = append(items, Item{ID: "login/" + name, Name: name, Source: SourceThirdParty, Kind: "login", Scope: "用户", State: "登录时打开", StartMode: "登录项", Command: path, Path: path, Startup: true})
	}
	return items
}

// classify 区分 Apple 自带服务、应用实例与第三方服务；只有第三方服务提供修改操作
func classify(item *Item) {
	item.Scope = "用户"
	if item.Domain == "system" {
		item.Scope = "系统"
		item.Admin = true
	}
	switch {
	case strings.HasPrefix(item.Name, "com.apple.") || strings.HasPrefix(item.Path, "/System/"):
		item.Source = SourceApple
	case strings.HasPrefix(item.Name, "application."):
		item.Source = SourceApp
		item.Name = appName(item.Name)
	default:
		item.Source = SourceThirdParty
	}
}

// appName 把 application.com.tencent.xinWeChat.396.397 还原为应用标识
func appName(label string) string {
	parts := strings.Split(strings.TrimPrefix(label, "application."), ".")
	for len(parts) > 1 {
		if _, err := strconv.ParseUint(parts[len(parts)-1], 10, 64); err != nil {
			break
		}
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
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
		item := Item{ID: domain + "/" + name, Name: name, Domain: domain, State: "未运行", PID: pid, Kind: "launchd"}
		status := fields[1]
		switch {
		case pid > 0:
			item.State, item.Running = "运行中", true
		case status == "0" || status == "-" || status == "(pe)":
		case status == "-15" || status == "-2":
			// 被 SIGTERM 或 SIGINT 结束通常是正常停止
			item.State = "已停止（信号 " + strings.TrimPrefix(status, "-") + "）"
		default:
			item.State, item.Failed = "异常退出（"+status+"）", true
		}
		items = append(items, item)
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		return Item{}, err
	}
	if fields == nil {
		return Item{}, fmt.Errorf("plist 根节点必须是字典")
	}
	// 空字典符合 plist 格式，但没有可展示、可操作的服务，静默跳过。
	if len(fields) == 0 {
		return Item{}, errPlaceholderPlist
	}
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
	mode, short := "按需启动", "按需"
	if data.RunAtLoad {
		mode, short = "自动启动", "自动"
	}
	if data.KeepAlive != nil && data.KeepAlive != false {
		mode = short + " · 保活"
	}
	item := Item{ID: domain + "/" + data.Label, Name: data.Label, Domain: domain, Command: command, Path: path, StartMode: mode, Startup: true, Disabled: data.Disabled, Kind: "plist"}
	for _, p := range []string{data.StandardOutPath, data.StandardErrorPath} {
		if filepath.IsAbs(p) && (len(item.LogPaths) == 0 || item.LogPaths[0] != p) {
			item.LogPaths = append(item.LogPaths, p)
		}
	}
	return item, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func appleString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// asAdmin 通过系统授权对话框以管理员身份执行，参数逐个转义，不拼接用户输入
func asAdmin(c sysx.Cmd) sysx.Cmd {
	parts := []string{shellQuote(c.Name)}
	for _, a := range c.Args {
		parts = append(parts, shellQuote(a))
	}
	return sysx.C("osascript", "-e", "do shell script "+appleString(strings.Join(parts, " "))+" with administrator privileges")
}

// Actions 只列出当前状态下可用的操作：运行中的提供停止与重启，未运行的提供启动
func Actions(item Item) []Action {
	var actions []Action
	if item.Kind == "login" {
		actions = append(actions, Action{Key: "d", Label: "移除登录项", Command: sysx.C("osascript", "-e", `tell application "System Events" to delete login item `+appleString(item.Name)), Mutates: true, Note: "从登录项中移除，不卸载应用；可在系统设置中重新添加"})
		if item.Path != "" {
			actions = append(actions, Action{Key: "o", Label: "显示应用", Command: sysx.C("open", "-R", item.Path)})
		}
		return actions
	}
	target := item.ID
	if item.Source == SourceThirdParty {
		switch {
		case item.Running:
			actions = append(actions,
				Action{Key: "s", Label: "停止", Command: sysx.C("launchctl", "bootout", target), Mutates: true, Note: "卸载服务并结束进程，保活设置不会再拉起；下次登录或手动启动前不再运行"},
				Action{Key: "R", Label: "重启", Command: sysx.C("launchctl", "kickstart", "-k", target), Mutates: true, Note: "结束当前进程并立即重新启动"})
		case item.State == "未加载":
			if item.Path != "" && !item.Disabled {
				actions = append(actions, Action{Key: "s", Label: "启动", Command: sysx.C("launchctl", "bootstrap", item.Domain, item.Path), Mutates: true, Note: "加载配置文件并按配置启动"})
			}
		default:
			actions = append(actions, Action{Key: "s", Label: "启动", Command: sysx.C("launchctl", "kickstart", target), Mutates: true, Note: "立即启动服务"})
		}
		if item.Disabled {
			actions = append(actions, Action{Key: "e", Label: "启用", Command: sysx.C("launchctl", "enable", target), Mutates: true, Note: "允许开机或登录时自动加载；不会立即启动"})
		} else {
			actions = append(actions, Action{Key: "e", Label: "禁用", Command: sysx.C("launchctl", "disable", target), Mutates: true, Note: "阻止开机或登录时自动加载；当前进程不受影响，可再按 s 停止"})
		}
		if item.Admin && os.Geteuid() != 0 {
			for i := range actions {
				actions[i].Preview = actions[i].Command.String() + "（管理员权限）"
				actions[i].Command = asAdmin(actions[i].Command)
				actions[i].Note += "；系统服务需要输入管理员密码"
			}
		}
	}
	if item.Path != "" {
		actions = append(actions, Action{Key: "o", Label: "显示配置", Command: sysx.C("open", "-R", item.Path)})
	}
	if item.State != "未加载" {
		actions = append(actions, Action{Key: "v", Label: "运行详情", Command: sysx.C("launchctl", "print", target)})
	}
	logs := sysx.C("/usr/bin/log", "show", "--last", "10m", "--style", "compact", "--predicate", "eventMessage CONTAINS[c] "+strconv.Quote(item.Name))
	note := "查询近 10 分钟包含服务标签的系统日志"
	if len(item.LogPaths) > 0 {
		logs = sysx.C("tail", append([]string{"-n", "200", "--"}, item.LogPaths...)...)
		note = "读取配置日志末尾 200 行"
	}
	return append(actions, Action{Key: "l", Label: "日志", Command: logs, Note: note})
}
