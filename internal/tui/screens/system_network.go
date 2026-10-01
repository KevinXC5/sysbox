package screens

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/tools/network"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

const proxyCompareTarget = "https://www.google.com"

// networkState 保存已完成的检测，重新进入或单项重测时不必全部重跑
type networkState struct {
	mu      sync.Mutex
	custom  []string
	reports map[string]network.Report
	pending map[string]bool
	checked map[string]time.Time
	proxy   network.ProxySettings
}

func NewNetwork(env Env) Page {
	s := &networkState{reports: map[string]network.Report{}, pending: map[string]bool{}, checked: map[string]time.Time{}}
	spec := systemSpec{name: "网络诊断", tabs: []string{"连通性", "代理", "本机网络"}, timeout: 40 * time.Second}
	spec.columns = func(tab int) []systemColumn {
		switch tab {
		case 0:
			return []systemColumn{{title: "目标"}, {title: "结果", width: 22}, {title: "耗时", width: 9, right: true}, {title: "线路"}, {title: "失败环节", width: 10}, {title: "检测时间", width: 8}}
		case 1:
			return []systemColumn{{title: "项目", width: 18}, {title: "状态", width: 12}, {title: "内容"}}
		}
		return []systemColumn{{title: "项目", width: 14}, {title: "内容"}}
	}
	spec.input = func(tab int) string {
		return map[int]string{0: "添加目标", 1: "对比目标"}[tab]
	}
	spec.submit = func(tab int, value string) (int, string) {
		if tab == 0 {
			s.add(value)
			return 0, ""
		}
		return tab, value
	}
	spec.load = func(ctx context.Context, tab int, target string, force bool) (systemResult, error) {
		switch tab {
		case 0:
			return s.rows(), nil
		case 1:
			return s.loadProxy(ctx, target)
		}
		return loadLocalNetwork(ctx)
	}
	spec.actions = func(tab int, row systemRow) []systemAction {
		switch tab {
		case 0:
			actions := []systemAction{{key: "enter", label: "链路详情"}, {key: "r", label: "重测当前"}, {key: "p", label: "对比线路"}}
			if s.isCustom(row.key) {
				actions = append(actions, systemAction{key: "x", label: "移除目标"})
			}
			return actions
		case 1:
			s.mu.Lock()
			command := network.ExportCommand(s.proxy, runtime.GOOS == "windows")
			s.mu.Unlock()
			if command == "" {
				return nil
			}
			return []systemAction{{key: "c", label: "复制终端代理命令", quiet: true, run: func(ctx context.Context) (string, error) {
				if err := copyText(ctx, command); err != nil {
					return "", err
				}
				return "已复制：" + command + "；在执行命令的终端重新启动 sysbox 后生效", nil
			}}}
		}
		return nil
	}
	p := &networkPage{systemPage: newSystemPage(env, spec), state: s, checks: map[string]networkCheck{}, limit: make(chan struct{}, 6)}
	p.spec.panes = func(base *systemPage, w, h int) string { return base.listPane(w, h) }
	return p
}

func (s *networkState) isCustom(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.custom {
		if t == target {
			return true
		}
	}
	return false
}

func (s *networkState) add(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.targets() {
		if t.target == target {
			delete(s.reports, target)
			return
		}
	}
	s.custom = append([]string{target}, s.custom...)
}

func (s *networkState) remove(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.custom[:0]
	for _, t := range s.custom {
		if t != target {
			kept = append(kept, t)
		}
	}
	s.custom = kept
	delete(s.reports, target)
	delete(s.pending, target)
	delete(s.checked, target)
}

type networkTarget struct{ name, target string }

func (s *networkState) targets() []networkTarget {
	var out []networkTarget
	for _, t := range s.custom {
		out = append(out, networkTarget{t, t})
	}
	for _, p := range network.Presets {
		out = append(out, networkTarget{p.Name, p.Target})
	}
	return out
}

func (s *networkState) rows() systemResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := systemResult{}
	ok, failed := 0, 0
	for _, t := range s.targets() {
		row := systemRow{name: t.name, key: t.target, ratio: -1}
		report, done := s.reports[t.target]
		if !done {
			status := "未检测"
			if s.pending[t.target] {
				status = "检测中…"
			}
			row.cells, row.tone = []string{status, "", "", "", ""}, theme.Muted
			row.fields = []containers.Field{{Label: "地址", Value: t.target}, {Label: "状态", Value: status}}
			result.rows = append(result.rows, row)
			continue
		}
		if report.OK() {
			ok++
		} else {
			failed++
		}
		row.cells, row.tone = reportCells(report)
		row.cells = append(row.cells, report.Failed, s.checked[t.target].Format("15:04:05"))
		row.fields = reportFields(report, s.proxy)
		result.rows = append(result.rows, row)
	}
	route := "直连"
	if network.EnvProxySet() {
		route = "按终端代理变量"
	}
	result.info = fmt.Sprintf("可访问 %d · 失败 %d · 待完成 %d · 线路：%s", ok, failed, len(result.rows)-ok-failed, route)
	if failed > 0 && s.proxy.Enabled() && !network.EnvProxySet() {
		result.warn = "系统代理已开启，终端未设置代理；选中失败目标按 p 对比线路"
	}
	return result
}

func reportCells(r network.Report) ([]string, lipgloss.TerminalColor) {
	total := r.Total.Round(time.Millisecond).String()
	if !r.OK() {
		reason := ""
		if len(r.Steps) > 0 {
			reason = r.Steps[len(r.Steps)-1].Detail
		}
		return []string{"✗ " + r.Failed + " " + reason, total, r.Route}, theme.Rose
	}
	status := "✓ " + r.Status
	if code, _, ok := strings.Cut(r.Status, " "); ok {
		status = "✓ " + code
	}
	tone := lipgloss.TerminalColor(theme.Green)
	if strings.HasPrefix(r.Status, "5") {
		tone = theme.Amber
	}
	return []string{status, total, r.Route}, tone
}

func reportFields(r network.Report, proxy network.ProxySettings) []containers.Field {
	conclusion := "可以访问 · " + r.Status
	if !r.OK() {
		conclusion = "在「" + r.Failed + "」环节失败"
	}
	fields := []containers.Field{{Value: "结论"}, {Label: "结果", Value: conclusion}}
	if hint := reportHint(r, proxy); hint != "" {
		fields = append(fields, containers.Field{Label: "建议", Value: hint})
	}
	fields = append(fields, containers.Field{Value: "链路"})
	for _, step := range r.Steps {
		mark := "✓"
		if !step.OK {
			mark = "✗"
		}
		value := mark
		if step.Duration > 0 {
			value += " " + step.Duration.Round(time.Millisecond).String()
		}
		if step.Detail != "" {
			value += " · " + step.Detail
		}
		fields = append(fields, containers.Field{Label: step.Name, Value: value})
	}
	fields = append(fields, containers.Field{Value: "详情"}, containers.Field{Label: "地址", Value: r.Address}, containers.Field{Label: "线路", Value: r.Route}, containers.Field{Label: "解析结果", Value: strings.Join(r.IPs, "\n")}, containers.Field{Label: "总耗时", Value: r.Total.Round(time.Millisecond).String()})
	if r.Err != nil {
		fields = append(fields, containers.Field{Label: "原始错误", Value: r.Err.Error()})
	}
	return fields
}

func reportHint(r network.Report, proxy network.ProxySettings) string {
	if r.OK() {
		return ""
	}
	switch {
	case r.Route == "直连" && proxy.Enabled():
		return "系统代理已开启，但终端程序不会自动使用。按 p 对比当前目标的线路"
	case r.Failed == "DNS":
		return "检查网络连接和 DNS 服务器，见「本机网络」"
	case r.Failed == "连接代理":
		return "代理软件可能没有运行，或代理端口不正确"
	case r.Failed == "TLS":
		return "证书校验失败，可能被网络中间设备拦截，或系统时间不正确"
	case r.Failed == "TCP" || r.Failed == "连接":
		return "目标端口不可达；本机服务可在进程管理中按端口查找"
	}
	return ""
}

// loadProxy 对比系统代理与终端代理变量，并用直连、系统代理、终端设置分别访问同一目标
func (s *networkState) loadProxy(ctx context.Context, target string) (systemResult, error) {
	if target == "" {
		target = proxyCompareTarget
	}
	proxy, proxyErr := network.SystemProxy(ctx)
	s.mu.Lock()
	s.proxy = proxy
	s.mu.Unlock()
	envSet := network.EnvProxySet()
	type check struct {
		name   string
		via    network.Via
		report network.Report
		skip   string
	}
	checks := []*check{{name: "直连", via: network.Via{Mode: network.RouteDirect}}, {name: "经系统代理", via: network.Via{Mode: network.RouteProxy, Proxy: proxy.URL()}}, {name: "按终端设置", via: network.Via{Mode: network.RouteEnv}}}
	if !proxy.Enabled() {
		checks[1].skip = "系统代理未开启"
	}
	if !envSet {
		checks[2].skip = "终端未设置代理，与直连相同"
	}
	var wg sync.WaitGroup
	for _, c := range checks {
		if c.skip != "" {
			continue
		}
		wg.Add(1)
		go func(c *check) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			c.report = network.Check(checkCtx, target, c.via)
		}(c)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return systemResult{}, ctx.Err()
	}
	direct, viaSystem, viaEnv := checks[0].report, checks[1].report, checks[2].report
	command := network.ExportCommand(proxy, runtime.GOOS == "windows")
	level, title, detail := theme.Green, "终端代理工作正常", "终端程序按代理变量访问目标"
	switch {
	case proxy.Enabled() && !envSet && !direct.OK() && viaSystem.OK():
		level, title, detail = theme.Amber, "终端未使用系统代理", "浏览器经系统代理可以访问，git、npm、curl 等命令行工具会直连并失败。按 c 复制设置命令粘贴到终端；写入 ~/.zshrc 或 PowerShell 配置文件可长期生效"
	case proxy.Enabled() && !envSet && !direct.OK():
		level, title, detail = theme.Rose, "直连与系统代理都无法访问", "检查代理软件是否运行，或换一个对比目标"
	case proxy.Enabled() && !envSet:
		level, title, detail = theme.Blue, "终端直连可以访问", "系统代理已开启，终端未设置代理；需要终端走代理时按 c 复制设置命令"
	case envSet && !viaEnv.OK():
		level, title, detail = theme.Rose, "终端代理不可用", "代理变量指向的代理无法访问目标，检查代理软件是否运行、地址是否正确"
	case !proxy.Enabled() && !envSet && direct.OK():
		level, title, detail = theme.Green, "直连正常，未使用代理", "系统与终端都没有配置代理"
	case !proxy.Enabled() && !envSet:
		level, title, detail = theme.Rose, "直连失败，未配置代理", "目标无法直连访问；如需代理，先在系统设置中开启"
	}
	statusName := map[lipgloss.TerminalColor]string{theme.Green: "正常", theme.Blue: "提示", theme.Amber: "需要处理", theme.Rose: "异常"}
	conclusion := systemRow{name: "结论", key: "conclusion", cells: []string{statusName[level], title}, tone: level, ratio: -1, fields: []containers.Field{{Value: "结论"}, {Label: "状态", Value: title}, {Label: "说明", Value: detail}, {Label: "对比目标", Value: target}}}
	if command != "" {
		conclusion.fields = append(conclusion.fields, containers.Field{Label: "设置命令", Value: command})
	}
	result := systemResult{rows: []systemRow{conclusion}, info: "对比目标：" + target + " · 按 g 更换"}
	if proxyErr != nil {
		result.warn = "系统代理读取失败：" + proxyErr.Error()
	}
	add := func(name, value, kind string) {
		state, tone := "未开启", lipgloss.TerminalColor(theme.Muted)
		if value != "" {
			state, tone = "已开启", theme.Green
		}
		if kind == "终端" {
			state = strings.Replace(state, "开启", "设置", 1)
		}
		result.rows = append(result.rows, systemRow{name: name, key: name, cells: []string{state, value}, tone: tone, ratio: -1, fields: []containers.Field{{Value: kind + "代理"}, {Label: "项目", Value: name}, {Label: "状态", Value: state}, {Label: "地址", Value: value}}})
	}
	add("系统 HTTP", proxy.HTTP, "系统")
	add("系统 HTTPS", proxy.HTTPS, "系统")
	add("系统 SOCKS", proxy.SOCKS, "系统")
	if proxy.PAC != "" {
		add("系统自动配置", proxy.PAC, "系统")
	}
	if len(proxy.Exceptions) > 0 {
		result.rows[len(result.rows)-1].fields = append(result.rows[len(result.rows)-1].fields, containers.Field{Label: "不走代理", Value: strings.Join(proxy.Exceptions, ", ")})
	}
	for _, item := range network.EnvProxies() {
		add("终端 "+item.Key, item.Value, "终端")
	}
	for _, c := range checks {
		row := systemRow{name: "检测：" + c.name, key: "check-" + c.name, ratio: -1}
		if c.skip != "" {
			row.cells, row.tone = []string{"跳过", c.skip}, theme.Muted
			row.fields = []containers.Field{{Label: "说明", Value: c.skip}}
		} else {
			cells, tone := reportCells(c.report)
			row.cells, row.tone = []string{cells[0], cells[1] + " · " + cells[2]}, tone
			row.value = c.report
			row.fields = reportFields(c.report, network.ProxySettings{})
		}
		result.rows = append(result.rows, row)
	}
	return result, nil
}

func loadLocalNetwork(ctx context.Context) (systemResult, error) {
	info, err := network.Local(ctx)
	interfaces, ifErr := network.Interfaces()
	if ifErr != nil {
		return systemResult{}, ifErr
	}
	network.SortInterfaces(interfaces, info.GatewayInterface)
	result := systemResult{info: "默认路由所在的网卡排在最前"}
	if err != nil {
		result.warn = "网关或 DNS 读取不完整：" + err.Error()
	}
	gateway := info.Gateway
	if info.GatewayInterface != "" {
		gateway += "（" + info.GatewayInterface + "）"
	}
	result.rows = append(result.rows,
		systemRow{name: "默认网关", key: "gateway", cells: []string{gateway}, ratio: -1, fields: []containers.Field{{Value: "路由"}, {Label: "网关", Value: info.Gateway}, {Label: "网卡", Value: info.GatewayInterface}}},
		systemRow{name: "DNS 服务器", key: "dns", cells: []string{strings.Join(info.DNS, ", ")}, ratio: -1, fields: []containers.Field{{Value: "DNS"}, {Label: "服务器", Value: strings.Join(info.DNS, "\n")}}})
	for _, item := range interfaces {
		name := item.Name
		if item.Name == info.GatewayInterface {
			name += " · 默认"
		}
		result.rows = append(result.rows, systemRow{name: name, key: item.Name, cells: []string{strings.Join(item.Addrs, ", ")}, ratio: -1, fields: []containers.Field{{Value: "网卡"}, {Label: "名称", Value: item.Name}, {Label: "地址", Value: strings.Join(item.Addrs, "\n")}, {Label: "MAC", Value: item.MAC}}})
	}
	return result, nil
}

// copyText 写入系统剪贴板
func copyText(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "clip")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
