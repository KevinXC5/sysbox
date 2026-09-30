package screens

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/tools/containers"
)

type resourceRunner struct {
	calls  []sysx.Cmd
	output string
	err    error
}

func (r *resourceRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	r.calls = append(r.calls, c)
	return r.output, r.err
}
func pressResource(p *resourcePage, key string) tea.Cmd {
	if key == "enter" {
		return p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if key == "esc" {
		return p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
	keys := map[string]tea.KeyType{"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown}
	if keyType, ok := keys[key]; ok {
		return p.Update(tea.KeyMsg{Type: keyType})
	}
	return p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}
func TestResourceShortcutsAndDryRun(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	runner := &resourceRunner{output: "容器日志"}
	p.client.Runner = runner
	p.items = []containers.Item{{ID: "abc", Name: "test", State: "running"}}
	cmd := pressResource(p, "l")
	if cmd == nil || p.busy || !p.logAlive {
		t.Fatal("实时日志应一个按键开始，接收期间不锁定操作")
	}
	p.Update(cmd())
	if p.mode != resourceList || p.pane != "logs" || p.outputText != "容器日志" {
		t.Fatal("日志应就地显示，不能跳转操作菜单")
	}
	if strings.Contains(p.Body(120, 30), "docker --context") || strings.Contains(p.outputText, "docker logs") {
		t.Fatal("不能显示底层命令")
	}
	cmd = pressResource(p, "s")
	if cmd == nil || p.mode == resourceConfirm {
		t.Fatal("停止容器不能增加确认步骤")
	}
	p.Update(cmd())
	if len(runner.calls) != 1 || !strings.Contains(p.notice, "演练") || strings.Contains(p.notice, "docker") {
		t.Fatal("演练不应修改或展示命令")
	}
	cmd = pressResource(p, "p")
	if cmd == nil || p.mode != resourceInput {
		t.Fatal("拉取镜像应直接进入就地输入")
	}
	body := p.Body(120, 30)
	if !strings.Contains(body, "镜像名称") || !strings.Contains(body, "test") {
		t.Fatal("输入时应保留资源列表")
	}
}
func TestResourceDeleteInlineConfirmation(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	runner := &resourceRunner{}
	p.client.Runner = runner
	p.items = []containers.Item{{ID: "abc", Name: "test", State: "exited"}}
	if cmd := pressResource(p, "d"); cmd != nil || p.mode != resourceConfirm || len(runner.calls) != 0 {
		t.Fatal("删除应先就地确认")
	}
	body := p.Body(120, 30)
	if !strings.Contains(body, "test") || !strings.Contains(body, "y 确认") || strings.Contains(body, "docker rm") {
		t.Fatal("确认应只显示目标与操作说明")
	}
	pressResource(p, "esc")
	if p.mode != resourceList || len(runner.calls) != 0 {
		t.Fatal("取消不能执行删除")
	}
	pressResource(p, "d")
	cmd := pressResource(p, "y")
	if cmd == nil {
		t.Fatal("确认后应执行")
	}
	p.Update(cmd())
	if len(runner.calls) != 0 || !strings.Contains(p.notice, "演练") {
		t.Fatal("演练删除不能修改资源")
	}
}
func TestResourceTerminalOneKeyAndRootMessage(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	p.items = []containers.Item{{ID: "abc", Name: "test", State: "running"}}
	cmd := pressResource(p, "e")
	if cmd == nil {
		t.Fatal("终端应一个按键打开")
	}
	terminal, ok := cmd().(TerminalMsg)
	if !ok || terminal.Command.Path == "" || terminal.Done == nil {
		t.Fatal("终端必须交由根模型释放终端后执行")
	}
	if !p.busy {
		t.Fatal("交互期间应锁定页面")
	}
	p.Update(terminal.Done(fmt.Errorf("测试错误")))
	if p.err == nil || !strings.Contains(p.err.Error(), "测试错误") {
		t.Fatal("交互失败应就地显示原因")
	}
}
func TestResourcePodContainerSelection(t *testing.T) {
	p := newResourcePage(Env{}, true)
	defer p.Close()
	p.tab = 2
	p.client.Target = "test"
	p.items = []containers.Item{{ID: "abc", Name: "pod", Namespace: "team", Containers: []string{"app", "sidecar"}}}
	pressResource(p, "e")
	if p.mode != resourcePicker || p.picker != "container" {
		t.Fatal("多容器 Pod 应就地选择容器")
	}
	pressResource(p, "down")
	cmd := pressResource(p, "enter")
	terminal, ok := cmd().(TerminalMsg)
	if !ok || !strings.Contains(strings.Join(terminal.Command.Args, " "), "-c sidecar") {
		t.Fatal("应进入所选容器")
	}
}
func TestResourceBulkOnlyCommonActions(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	p.items = []containers.Item{{ID: "a", Name: "running", State: "running"}, {ID: "b", Name: "stopped", State: "exited"}}
	for _, item := range p.items {
		p.selected[itemKey(item)] = true
	}
	for _, shortcut := range p.shortcuts() {
		if shortcut.id == "start" || shortcut.id == "stop" || shortcut.id == "restart" {
			t.Fatalf("状态不同的批量资源不能显示非共用操作：%s", shortcut.id)
		}
	}
	if _, items, ok := p.findAction("logs"); !ok || len(items) != 1 || items[0].ID != "a" {
		t.Fatal("多选时日志应针对当前光标项")
	}
	if cmd := pressResource(p, "s"); cmd != nil {
		t.Fatal("不应对混合状态的资源批量启停")
	}
}
func TestResourceAutomaticDetailsAndStaleResponse(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	runner := &resourceRunner{output: `[{"Id":"abc","State":{"Status":"running"},"Config":{"Image":"nginx:test"},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}}}]`}
	p.client.Runner = runner
	p.Update(resourceLoadedMsg{items: []containers.Item{{ID: "abc", Name: "first", State: "running"}, {ID: "def", Name: "second", State: "running"}}})
	cmd := p.Update(resourceDetailTick{seq: p.detailSeq, item: p.items[0]})
	if cmd == nil {
		t.Fatal("资源加载后应自动获取选中项信息")
	}
	loaded := cmd()
	pressResource(p, "down")
	p.Update(loaded)
	if len(p.details) != 0 {
		t.Fatal("旧资源的迟到详情不能覆盖新选择")
	}
	seq := p.detailSeq
	p.Update(resourceDetailMsg{seq: seq, key: itemKey(p.items[1]), fields: []containers.Field{{Label: "名称", Value: "second"}, {Label: "端口", Value: "127.0.0.1:8080"}}})
	body := p.Body(120, 30)
	if !strings.Contains(body, "基础信息") || !strings.Contains(body, "127.0.0.1:8080") || strings.Contains(body, `"State"`) {
		t.Fatal("应自动用信息表展示详情")
	}
}
func TestResourceAllNamespaceAndRefreshSelection(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, true)
	defer p.Close()
	p.client.Target = "test"
	p.namespace = "*"
	p.items = []containers.Item{{ID: "a", Name: "app", Namespace: "team", State: "1"}}
	cmd := pressResource(p, "R")
	if cmd == nil || !strings.Contains(p.plans[0].Command.String(), "--namespace team") {
		t.Fatal("操作必须使用资源实际命名空间")
	}
	p.Update(cmd())
	p.selected[itemKey(p.items[0])] = true
	p.Update(resourceLoadedMsg{})
	if len(p.selected) != 0 || len(p.items) != 0 {
		t.Fatal("刷新后应清除消失资源的选择")
	}
}
func TestResourceLayoutAndNoCommandText(t *testing.T) {
	for _, kubernetes := range []bool{false, true} {
		p := newResourcePage(Env{}, kubernetes)
		defer p.Close()
		for i := 0; i < 100; i++ {
			p.items = append(p.items, containers.Item{ID: fmt.Sprint(i), Name: strings.Repeat("中文", 50), State: "running", Columns: []string{strings.Repeat("image", 30), "running", "127.0.0.1:8080"}})
		}
		for _, mode := range []resourceMode{resourceList, resourceInput, resourcePicker, resourceConfirm} {
			p.mode = mode
			p.action = containers.Action{Label: "删除", Prompt: "容器名称（使用镜像默认命令）"}
			p.actionItems = []containers.Item{p.items[0]}
			p.choices = []string{"test"}
			for _, size := range [][2]int{{60, 16}, {100, 30}, {197, 58}} {
				body := p.Body(size[0], size[1])
				if lipgloss.Width(body) > size[0] || lipgloss.Height(body) > size[1] {
					t.Fatalf("模式 %d 超出 %v：%d×%d", mode, size, lipgloss.Width(body), lipgloss.Height(body))
				}
			}
		}
		p.mode = resourceList
		p.showOutput("日志", "\x1b[31m日志\x1b[0m\x1b[2J\x07"+strings.Repeat("longtext", 40))
		if strings.ContainsAny(p.outputText, "\x1b\x07") {
			t.Fatal("日志不能包含终端控制序列")
		}
		body := p.Body(60, 16)
		if lipgloss.Width(body) > 60 || lipgloss.Height(body) > 16 {
			t.Fatal("日志分屏超出窗口")
		}
	}
}

func TestResourcePullEntryUsesCurrentImageDefault(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	p.tab = 1
	p.items = []containers.Item{{ID: "app:v1", Name: "app:v1"}}
	runner := &resourceRunner{}
	p.client.Runner = runner
	pressResource(p, "p")
	if p.mode != resourceInput || p.input.Value() != "app:v1" {
		t.Fatal("拉取镜像应就地输入并填入当前镜像，可直接确认或改名")
	}
	cmd := pressResource(p, "enter")
	if cmd == nil || p.mode == resourceConfirm {
		t.Fatal("确认名称后应直接拉取")
	}
	if !strings.Contains(p.plans[0].Command.String(), "pull app:v1") {
		t.Fatal("应拉取输入的镜像")
	}
	p.Update(cmd())
	if len(runner.calls) != 0 || !strings.Contains(p.notice, "演练") {
		t.Fatal("演练拉取不能执行修改")
	}
}

func TestResourceFuzzyQueryAcrossKinds(t *testing.T) {
	tests := []struct {
		kubernetes bool
		tab        int
		query      string
		fields     []containers.Item
		want       string
	}{
		{false, 0, "ngnx run", []containers.Item{{ID: "abc", Name: "nginx-web", State: "running"}, {ID: "def", Name: "other", State: "running"}}, "nginx-web"},
		{false, 1, "tvhk", []containers.Item{{ID: "abc", Name: "nginx:1.29"}, {ID: "def", Name: "ghcr.io/team/tavily-hikari:latest"}}, "ghcr.io/team/tavily-hikari:latest"},
		{true, 2, "api prod", []containers.Item{{ID: "abc", Name: "api-server-123", Namespace: "production"}, {ID: "def", Name: "api-server-456", Namespace: "staging"}}, "api-server-123"},
		{true, 0, "web def", []containers.Item{{ID: "abc", Name: "web-frontend", Namespace: "default"}, {ID: "def", Name: "worker", Namespace: "default"}}, "web-frontend"},
	}
	for _, test := range tests {
		p := newResourcePage(Env{}, test.kubernetes)
		p.tab = test.tab
		p.items = test.fields
		pressResource(p, "/")
		pressResource(p, test.query)
		items := p.visible()
		if len(items) != 1 || items[0].Name != test.want {
			t.Fatalf("资源模糊查询失败：%q → %+v", test.query, items)
		}
		if p.mode != resourceInput || !p.Typing() {
			t.Fatal("查询输入应保持焦点，字母不能触发操作")
		}
		pressResource(p, "enter")
		if p.filter != test.query || p.mode != resourceList {
			t.Fatal("Enter 应保留查询并恢复快捷键")
		}
		pressResource(p, "/")
		pressResource(p, "esc")
		if p.filter != "" || len(p.visible()) != len(test.fields) {
			t.Fatal("Esc 应一次清除查询")
		}
		p.Close()
	}
}
func TestResourceDetailSearchAndGroups(t *testing.T) {
	p := newResourcePage(Env{}, true)
	defer p.Close()
	p.tab = 2
	p.items = []containers.Item{{ID: "abc", Name: "pod", Fields: []containers.Field{{Label: "名称", Value: "pod"}, {Value: "卷与挂载"}, {Label: "挂载", Value: "app-data → /srv/app/data"}, {Value: "运行配置"}, {Label: "工作目录", Value: "/srv/app"}}}}
	pressResource(p, "f")
	pressResource(p, "app-data")
	body := p.Body(120, 30)
	if !strings.Contains(body, "app-data") || !strings.Contains(body, "卷与挂载") || strings.Contains(body, "工作目录") {
		t.Fatal("详情应按内容过滤，保留相关分组")
	}
	pressResource(p, "enter")
	if p.detailQuery != "app-data" || p.mode != resourceList {
		t.Fatal("详情查询完成后应保留结果")
	}
	pressResource(p, "esc")
	if p.detailQuery != "" || !strings.Contains(p.Body(120, 30), "工作目录") {
		t.Fatal("Esc 应直接恢复完整信息表")
	}
	p.showOutput("日志", "first-line\nconnection refused\nrequest completed")
	pressResource(p, "f")
	pressResource(p, "cnn ref")
	body = p.Body(120, 30)
	if !strings.Contains(body, "connection refused") || strings.Contains(body, "request completed") {
		t.Fatal("日志也应支持模糊查询")
	}
}
func TestResourcePickerFuzzyQueryAndExactSelection(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, true)
	defer p.Close()
	p.startPicker("namespace", []string{"*", "default", "production", "staging"})
	pressResource(p, "prd")
	values := p.visibleChoices()
	if len(values) != 1 || values[0] != "production" || !p.Typing() {
		t.Fatal("命名空间应直接输入模糊查询")
	}
	if cmd := pressResource(p, "enter"); cmd == nil || p.namespace != "production" {
		t.Fatal("查询后应选中真实命名空间")
	}
	p.busy = false
	p.tab = 2
	p.items = []containers.Item{{ID: "abc", Name: "pod", Namespace: "team", Containers: []string{"api", "metrics-sidecar"}}}
	pressResource(p, "e")
	pressResource(p, "mtss")
	if values := p.visibleChoices(); len(values) != 1 || values[0] != "metrics-sidecar" {
		t.Fatal("Pod 容器选择应支持模糊查询")
	}
	cmd := pressResource(p, "enter")
	if cmd == nil || !strings.Contains(p.plans[0].Command.String(), "-c metrics-sidecar") {
		t.Fatal("应对过滤后的真实容器执行操作")
	}
}

func TestResourcePullAllShortcutAndDryRun(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	runner := &resourceRunner{output: `{"Repository":"app","Tag":"v1","ID":"abc"}
{"Repository":"other","Tag":"v2","ID":"def"}`}
	p.client.Runner = runner
	cmd := pressResource(p, "P")
	if cmd == nil || !p.busy || p.mode == resourceInput {
		t.Fatal("拉取所有镜像应直接开始，不应要求输入其他镜像")
	}
	for cmd != nil {
		cmd = p.Update(cmd())
	}
	if len(runner.calls) != 1 || !strings.Contains(p.notice, "2 个镜像") || p.busy {
		t.Fatal("全部拉取演练应只读取列表并显示数量")
	}
	hints := p.quickHints(160)
	if !strings.Contains(hints, "拉取所有镜像") || strings.Contains(hints, "其他镜像") {
		t.Fatal("工具栏应显示拉取镜像与拉取所有镜像")
	}
}
