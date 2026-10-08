package screens

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/ui/search"
)

type resourceMode int

const (
	resourceList resourceMode = iota
	resourceInput
	resourceConfirm
	resourcePicker
)

type resourceFocus int

const (
	resourceFocusLeft resourceFocus = iota
	resourceFocusRight
)

type resourceLoadedMsg struct {
	items []containers.Item
	err   error
}
type resourceTargetsMsg struct {
	targets []string
	current string
	err     error
}
type resourceNamespaceMsg struct {
	namespace string
	err       error
}
type resourceChoicesMsg struct {
	choices []string
	err     error
}
type resourcePullMsg containers.PullProgress

type resourceDoneMsg struct {
	output        string
	err           error
	refresh, read bool
	title         string
}
type resourceDetailTick struct {
	seq  int
	item containers.Item
}
type resourceDetailMsg struct {
	seq    int
	key    string
	fields []containers.Field
	err    error
}

type resourcePage struct {
	env                               Env
	client                            *containers.Client
	ctx                               context.Context
	cancel                            context.CancelFunc
	kinds, labels, targets            []string
	namespace                         string
	tab, cursor, offset               int
	focus                             resourceFocus
	listRows                          int
	items                             []containers.Item
	selected                          map[string]bool
	mode                              resourceMode
	busy                              bool
	err                               error
	notice                            string
	input                             textinput.Model
	inputPurpose, filter              string
	detailQuery, pickerQuery          string
	action                            containers.Action
	actionItems                       []containers.Item
	plans                             []containers.Plan
	choice                            int
	choices                           []string
	picker                            string
	output                            viewport.Model
	pane, outputTitle, outputText     string
	detailSeq                         int
	logSeq                            int
	logCancel                         context.CancelFunc
	logCh                             <-chan resourceLogEntry
	logAlive, logAutoScroll           bool
	logLines                          []string
	logBytes, logReceived, logPauseAt int
	logError                          error
	pullCh                            chan tea.Msg
	detailCancel                      context.CancelFunc
	detailPending                     bool
	detailError                       error
	details                           map[string][]containers.Field
}

func NewDocker(env Env) Page     { return newResourcePage(env, false) }
func NewKubernetes(env Env) Page { return newResourcePage(env, true) }
func newResourcePage(env Env, kubernetes bool) *resourcePage {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.CharLimit = 512
	input.Prompt = "› "
	p := &resourcePage{env: env, client: containers.New(kubernetes), ctx: ctx, cancel: cancel, namespace: "default", input: input, selected: map[string]bool{}, details: map[string][]containers.Field{}, output: viewport.New(80, 20), pane: "info"}
	p.kinds, p.labels = []string{"containers", "images"}, []string{"容器", "镜像"}
	if kubernetes {
		p.kinds, p.labels = []string{"deployments", "configmaps", "pods", "nodes"}, []string{"Deployment", "ConfigMap", "Pod", "Node"}
	}
	return p
}
func (p *resourcePage) Init() tea.Cmd {
	p.busy = true
	client, ctx := *p.client, p.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		targets, current, err := client.Targets(ctx)
		return resourceTargetsMsg{targets, current, err}
	}
}
func (p *resourcePage) Close() {
	p.stopLogs()
	p.cancel()
	if p.detailCancel != nil {
		p.detailCancel()
	}
}
func (p *resourcePage) Busy() bool   { return p.busy }
func (p *resourcePage) Typing() bool { return p.mode == resourceInput || p.mode == resourcePicker }
func (p *resourcePage) Crumbs() []string {
	name := "Docker 管理"
	if p.client.Kubernetes {
		name = "Kubernetes 管理"
	}
	return []string{"工具", name}
}
func (p *resourcePage) Status() string {
	if p.busy {
		return "正在执行…"
	}
	return fmt.Sprintf("%d 项 · 已选 %d 项", len(p.visible()), len(p.selected))
}
func (p *resourcePage) Hints() []string {
	if p.busy {
		return []string{"ctrl+c", "退出"}
	}
	switch p.mode {
	case resourceInput:
		return []string{"enter", "确定", "esc", "取消"}
	case resourceConfirm:
		return []string{"y", "确认", "esc", "取消"}
	case resourcePicker:
		return []string{"↑↓", "选择", "enter", "确定", "esc", "取消"}
	}
	move, page := "选择", "列表翻页"
	if p.focus == resourceFocusRight {
		move, page = "滚动", "内容翻页"
	}
	hints := []string{"tab", "资源", "←→", "面板", "↑↓", move, "PgUp/PgDn", page, "/", "模糊查询", "f", "详情查询", "r", "刷新", "c", "环境"}
	if p.pane == "logs" {
		hints = append(hints, "space", "自动滚动")
	}
	if p.client.Kubernetes {
		hints = append(hints, "n", "命名空间", "N", "输入命名空间")
	} else if p.pane != "logs" {
		hints = append(hints, "space", "多选")
	}
	return append(hints, "esc", "返回")
}

// useTarget 在切换集群后调用：Kubernetes 需先读取该集群 kubeconfig 中的默认命名空间，再刷新列表。
func (p *resourcePage) useTarget() tea.Cmd {
	if !p.client.Kubernetes {
		return p.reload()
	}
	p.busy, p.err = true, nil
	client, ctx := *p.client, p.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		namespace, err := client.CurrentNamespace(ctx)
		return resourceNamespaceMsg{namespace, err}
	}
}
func (p *resourcePage) reload() tea.Cmd {
	p.stopLogs()
	if p.pane == "logs" {
		p.pane = "info"
	}
	p.busy, p.err = true, nil
	p.invalidateDetails()
	client, kind, namespace, ctx := *p.client, p.kinds[p.tab], p.namespace, p.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		items, err := client.List(ctx, kind, namespace)
		return resourceLoadedMsg{items, err}
	}
}
func itemKey(item containers.Item) string { return item.Namespace + "/" + item.ID + "/" + item.Name }
func (p *resourcePage) visible() []containers.Item {
	if strings.TrimSpace(p.filter) == "" {
		return p.items
	}
	type match struct {
		item  containers.Item
		score int
	}
	var matches []match
	for _, item := range p.items {
		fields := []string{item.Name, item.ID, item.Namespace, item.State}
		fields = append(fields, item.Columns...)
		for _, field := range item.Fields {
			fields = append(fields, field.Value)
		}
		fields = append(fields, item.Containers...)
		if score, ok := search.Score(p.filter, fields...); ok {
			matches = append(matches, match{item, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	items := make([]containers.Item, 0, len(matches))
	for _, match := range matches {
		items = append(items, match.item)
	}
	return items
}
func (p *resourcePage) visibleChoices() []string {
	type match struct {
		value string
		score int
	}
	var matches []match
	for _, value := range p.choices {
		label := value
		if value == "*" {
			label = "全部命名空间"
		}
		if value == "" {
			label = "环境变量 默认连接"
		}
		if score, ok := search.Score(p.pickerQuery, label); ok {
			matches = append(matches, match{value, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, match.value)
	}
	return values
}
func (p *resourcePage) startPicker(picker string, choices []string) tea.Cmd {
	p.focus = resourceFocusRight
	p.mode, p.picker, p.choices, p.choice, p.pickerQuery = resourcePicker, picker, choices, 0, ""
	p.input.SetValue("")
	p.input.Placeholder = "直接输入名称模糊查询"
	p.input.CursorEnd()
	return p.input.Focus()
}

func (p *resourcePage) current() (containers.Item, bool) {
	items := p.visible()
	if len(items) == 0 {
		return containers.Item{}, false
	}
	return items[min(p.cursor, len(items)-1)], true
}
func (p *resourcePage) invalidateDetails() {
	p.detailSeq++
	if p.detailCancel != nil {
		p.detailCancel()
		p.detailCancel = nil
	}
	p.detailPending = false
	p.detailError = nil
	p.details = map[string][]containers.Field{}
}

// 光标快速移动时延迟查询 Docker 详情，并取消旧查询；列表中的基础信息立即可见。
func (p *resourcePage) queueDetails() tea.Cmd {
	p.stopLogs()
	p.detailSeq++
	if p.detailCancel != nil {
		p.detailCancel()
		p.detailCancel = nil
	}
	p.detailPending = false
	p.detailError = nil
	p.pane = "info"
	p.output.GotoTop()
	item, ok := p.current()
	if !ok || p.client.Kubernetes {
		return nil
	}
	if _, ok := p.details[itemKey(item)]; ok {
		return nil
	}
	p.detailPending = true
	seq := p.detailSeq
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return resourceDetailTick{seq, item} })
}
func (p *resourcePage) beginInput(purpose, prompt, value string) tea.Cmd {
	p.focus = resourceFocusRight
	if purpose == "filter" {
		p.focus = resourceFocusLeft
	}
	p.mode, p.inputPurpose = resourceInput, purpose
	p.input.Placeholder = prompt
	p.input.SetValue(value)
	p.input.CursorEnd()
	p.err = nil
	return p.input.Focus()
}
func (p *resourcePage) resetList() {
	p.focus = resourceFocusLeft
	p.stopLogs()
	p.mode = resourceList
	p.items = nil
	p.filter = ""
	p.detailQuery = ""
	p.selected = map[string]bool{}
	p.cursor, p.offset = 0, 0
	p.err = nil
	p.notice = ""
	p.pane = "info"
	p.invalidateDetails()
}
func (p *resourcePage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case resourceTargetsMsg:
		p.busy, p.err, p.targets = false, msg.err, msg.targets
		if msg.err != nil {
			return nil
		}
		if msg.current == "" && p.client.Kubernetes {
			if len(msg.targets) == 0 {
				p.err = fmt.Errorf("请配置 kubeconfig 后按 r 重试")
				return nil
			}
			msg.current = msg.targets[0]
		}
		p.client.Target = msg.current
		return p.useTarget()
	case resourceNamespaceMsg:
		// 读取失败时回退为 default，与 kubectl 的默认行为一致。
		p.namespace = "default"
		if msg.err == nil && msg.namespace != "" {
			p.namespace = msg.namespace
		}
		p.resetList()
		return p.reload()
	case resourceLoadedMsg:
		old, _ := p.current()
		p.busy, p.err, p.items = false, msg.err, msg.items
		keep := map[string]bool{}
		for i, item := range p.visible() {
			if itemKey(item) == itemKey(old) {
				p.cursor = i
			}
		}
		for _, item := range p.items {
			if p.selected[itemKey(item)] {
				keep[itemKey(item)] = true
			}
		}
		p.selected = keep
		p.cursor = min(p.cursor, max(0, len(p.visible())-1))
		return p.queueDetails()
	case resourceDetailTick:
		if msg.seq != p.detailSeq {
			return nil
		}
		item, ok := p.current()
		if !ok || itemKey(item) != itemKey(msg.item) {
			return nil
		}
		ctx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
		p.detailCancel = cancel
		client, kind, seq := *p.client, p.kinds[p.tab], p.detailSeq
		return func() tea.Msg {
			defer cancel()
			fields, err := client.Details(ctx, kind, item)
			return resourceDetailMsg{seq, itemKey(item), fields, err}
		}
	case resourceDetailMsg:
		if msg.seq != p.detailSeq {
			return nil
		}
		item, ok := p.current()
		if !ok || itemKey(item) != msg.key {
			return nil
		}
		p.detailPending, p.detailError = false, msg.err
		if msg.err == nil {
			p.details[msg.key] = msg.fields
		}
		return nil
	case resourceChoicesMsg:
		p.busy = false
		if msg.err != nil {
			p.err = fmt.Errorf("读取命名空间失败，可按 N 手动输入：%w", msg.err)
			return nil
		}
		return p.startPicker("namespace", msg.choices)
	case resourceLogMsg:
		if msg.seq != p.logSeq || p.pane != "logs" {
			return nil
		}
		p.appendLogs(msg.lines)
		if msg.done {
			p.logAlive = false
			p.logError = msg.err
			return nil
		}
		return p.waitLogs()
	case resourceSaveMsg:
		p.notice = fmt.Sprintf("打包镜像 %d/%d · %s", msg.Done+1, msg.Total, msg.Name)
		return p.waitPull()
	case resourcePullMsg:
		p.notice = fmt.Sprintf("拉取镜像 %d/%d · %s", msg.Done+1, msg.Total, msg.Name)
		return p.waitPull()
	case resourceDoneMsg:
		p.busy = false
		p.mode = resourceList
		if msg.read {
			p.showOutput(msg.title, msg.output)
			if msg.err != nil {
				p.showOutput(msg.title, msg.output+"\n操作失败，请检查目标状态和权限后重试：\n"+msg.err.Error())
			}
		} else {
			p.notice = msg.output
			if msg.err != nil {
				p.err = msg.err
			} else {
				p.err = nil
			}
		}
		if msg.refresh {
			p.selected = map[string]bool{}
			return p.reloadAfterAction(msg.err)
		}
		return nil
	case tea.KeyMsg:
		if p.busy {
			return nil
		}
		key := msg.String()
		if p.mode == resourceInput {
			return p.updateInput(msg)
		}
		if p.mode == resourceConfirm {
			if key == "esc" || key == "n" {
				p.mode = resourceList
				return nil
			}
			if key == "y" || key == "Y" {
				return p.execute()
			}
			if key == "up" || key == "down" || key == "pgup" || key == "pgdown" {
				return p.navigatePanel(key)
			}
			return nil
		}
		if p.mode == resourcePicker {
			return p.updatePicker(msg)
		}
		switch key {
		case "esc", "q":
			if p.detailQuery != "" {
				p.detailQuery = ""
				p.output.GotoTop()
				return nil
			}
			if p.pane != "info" {
				return p.queueDetails()
			}
			if p.filter != "" || len(p.selected) > 0 {
				p.filter = ""
				p.selected = map[string]bool{}
				p.cursor, p.offset = 0, 0
				return p.queueDetails()
			}
			return Back
		case "left":
			p.focus = resourceFocusLeft
		case "right":
			p.focus = resourceFocusRight
		case "tab", "shift+tab":
			step := 1
			if key == "shift+tab" {
				step = -1
			}
			p.tab = (p.tab + step + len(p.kinds)) % len(p.kinds)
			p.resetList()
			return p.reload()
		case "up", "k", "down", "j", "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
			return p.navigatePanel(key)

		case "enter", "b":
			return p.queueDetails()
		case "/":
			return p.beginInput("filter", "名称、ID、镜像、状态、多个关键词…", p.filter)
		case "f":
			return p.beginInput("detail", "字段或内容，多个关键词…", p.detailQuery)
		case "r":
			if len(p.targets) == 0 {
				return p.Init()
			}
			return p.reload()
		case "c":
			return p.startPicker("target", p.targets)
		case "n":
			if p.client.Kubernetes {
				p.busy = true
				client, ctx := *p.client, p.ctx
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
					defer cancel()
					choices, err := client.Namespaces(ctx)
					return resourceChoicesMsg{choices, err}
				}
			}
		case "N":
			if p.client.Kubernetes {
				return p.beginInput("namespace", "命名空间，* 表示全部", p.namespace)
			}
		case " ":
			if p.pane == "logs" {
				p.toggleLogScroll()
				return nil
			}
			if !p.client.Kubernetes {
				item, ok := p.current()
				if ok {
					key := itemKey(item)
					if p.selected[key] {
						delete(p.selected, key)
					} else {
						p.selected[key] = true
					}
				}
			}
		case "e", "E":
			if !p.client.Kubernetes && p.kinds[p.tab] == "images" {
				return p.beginSave(key == "E")
			}
			if key == "E" {
				return p.activate("exec", true)
			}
			return p.activate("exec", false)
		case "P":
			if !p.client.Kubernetes {
				return p.pullAll()
			}
		case "p":
			if !p.client.Kubernetes {
				p.action = containers.Action{ID: "pull", Label: "拉取镜像", Prompt: "镜像名称，例如 nginx:alpine", Mutates: true}
				p.actionItems = []containers.Item{{}}
				value := ""
				if item, ok := p.current(); ok && p.kinds[p.tab] == "images" && !strings.Contains(item.Name, "<none>") {
					value = item.Name
				}
				return p.beginInput("action", p.action.Prompt, value)
			}

		default:
			for _, shortcut := range p.shortcuts() {
				if shortcut.key == key {
					return p.activate(shortcut.id, false)
				}
			}
			if key == "E" {
				return p.activate("exec", true)
			}
		}
	}
	return nil
}

// 修改操作的结果就地显示，刷新不会吞掉错误提示。
func (p *resourcePage) reloadAfterAction(err error) tea.Cmd {
	cmd := p.reload()
	p.err = err
	return func() tea.Msg {
		msg := cmd().(resourceLoadedMsg)
		if msg.err == nil {
			msg.err = err
		}
		return msg
	}
}
func (p *resourcePage) updateInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "down":
		if p.inputPurpose == "filter" {
			if msg.String() == "up" {
				p.cursor = max(0, p.cursor-1)
			} else {
				p.cursor = min(max(0, len(p.visible())-1), p.cursor+1)
			}
			return p.queueDetails()
		}
		if p.inputPurpose == "detail" {
			p.pauseLogScroll()
			if msg.String() == "up" {
				p.output.LineUp(1)
			} else {
				p.output.LineDown(1)
			}
			return nil
		}
	case "pgup", "pgdown":
		return p.navigatePanel(msg.String())
	case "esc":
		if p.inputPurpose == "filter" {
			p.filter = ""
			p.cursor, p.offset = 0, 0
		}
		if p.inputPurpose == "detail" {
			p.detailQuery = ""
			p.input.Blur()
			p.mode = resourceList
			p.output.GotoTop()
			return nil
		}
		p.input.Blur()
		p.mode = resourceList
		p.err = nil
		return p.queueDetails()
	case "enter":
		value := strings.TrimSpace(p.input.Value())
		switch p.inputPurpose {
		case "filter":
			p.input.Blur()
			p.mode = resourceList
			return p.queueDetails()
		case "detail":
			p.input.Blur()
			p.mode = resourceList
			return nil
		case "save-directory":
			if value == "" {
				p.err = fmt.Errorf("请输入保存目录")
				return nil
			}
			return p.saveImages(value)
		case "namespace":
			if value == "" || strings.ContainsAny(value, " \t\r\n") || strings.HasPrefix(value, "-") {
				p.err = fmt.Errorf("请输入命名空间名称，或 * 查看全部")
				return nil
			}
			p.namespace = value
			p.input.Blur()
			p.resetList()
			return p.reload()
		default:
			return p.prepare(value)
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.inputPurpose == "filter" {
		p.filter = p.input.Value()
		p.cursor, p.offset = 0, 0
		return tea.Batch(cmd, p.queueDetails())
	}
	if p.inputPurpose == "detail" {
		p.detailQuery = p.input.Value()
		p.output.GotoTop()
	}
	return cmd
}
func (p *resourcePage) updatePicker(msg tea.KeyMsg) tea.Cmd {
	values := p.visibleChoices()
	switch msg.String() {
	case "esc":
		p.input.Blur()
		p.mode = resourceList
		p.pickerQuery = ""
		return nil
	case "up":
		p.choice = max(0, p.choice-1)
		return nil
	case "down":
		p.choice = min(max(0, len(values)-1), p.choice+1)
		return nil
	case "enter":
		if len(values) == 0 {
			return nil
		}
		value := values[min(p.choice, len(values)-1)]
		p.input.Blur()
		if p.picker == "container" {
			return p.prepare(value)
		}
		if p.picker == "namespace" {
			p.namespace = value
			p.resetList()
			return p.reload()
		}
		p.client.Target = value
		p.resetList()
		return p.useTarget()
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.pickerQuery = p.input.Value()
	p.choice = 0
	return cmd
}

func (p *resourcePage) actionTargets(a containers.Action) []containers.Item {
	item, ok := p.current()
	if !ok {
		return nil
	}
	if !a.Bulk || len(p.selected) == 0 {
		return []containers.Item{item}
	}
	var items []containers.Item
	for _, item := range p.items {
		if p.selected[itemKey(item)] {
			items = append(items, item)
		}
	}
	return items
}
func (p *resourcePage) findAction(id string) (containers.Action, []containers.Item, bool) {
	item, ok := p.current()
	if !ok {
		return containers.Action{}, nil, false
	}
	for _, a := range p.client.Actions(p.kinds[p.tab], item) {
		if a.ID != id {
			continue
		}
		targets := p.actionTargets(a)
		for _, target := range targets {
			found := false
			for _, other := range p.client.Actions(p.kinds[p.tab], target) {
				if other.ID == id {
					found = true
				}
			}
			if !found {
				return a, nil, false
			}
		}
		return a, targets, true
	}
	return containers.Action{}, nil, false
}
func (p *resourcePage) activate(id string, manual bool) tea.Cmd {
	action, items, ok := p.findAction(id)
	if !ok {
		p.notice = "当前资源或所选资源不支持此操作"
		return nil
	}
	if id == "logs" {
		return p.startLogs()
	}
	p.stopLogs()
	p.action, p.actionItems = action, items
	p.pane = "info"
	p.output.GotoTop()
	p.err = nil
	p.notice = ""
	if action.Prompt != "" {
		if id == "exec" && !manual {
			if p.client.Kubernetes && len(items[0].Containers) > 1 {
				return p.startPicker("container", items[0].Containers)
			}
			return p.prepare(action.Default)
		}
		return p.beginInput("action", action.Prompt, action.Default)
	}
	return p.prepare("")
}
func (p *resourcePage) prepare(value string) tea.Cmd {
	p.plans = nil
	for _, item := range p.actionItems {
		plan, err := p.client.Plan(p.kinds[p.tab], item, p.action, value)
		if err != nil {
			p.err = err
			return nil
		}
		p.plans = append(p.plans, plan)
	}
	p.input.Blur()
	p.err = nil
	// 删除与节点排空就地确认；日常操作直接执行，避免额外的弹窗和返回步骤。
	switch p.action.ID {
	case "rm", "rmi", "delete", "drain":
		p.mode = resourceConfirm
		p.focus = resourceFocusRight
		p.pane = "info"
		p.output.GotoTop()
		return nil
	}
	return p.execute()
}
func (p *resourcePage) execute() tea.Cmd {
	p.busy = true
	p.mode = resourceList
	client, plans, items, dryRun, ctx := *p.client, append([]containers.Plan(nil), p.plans...), append([]containers.Item(nil), p.actionItems...), p.env.DryRun, p.ctx
	action := p.action
	read := !action.Mutates
	if len(plans) == 1 && plans[0].Interactive && !dryRun {
		command := plans[0].Command
		return func() tea.Msg {
			return TerminalMsg{Command: exec.CommandContext(ctx, command.Name, command.Args...), Done: func(err error) tea.Msg {
				return resourceDoneMsg{output: action.Label + "已结束", err: err, refresh: true}
			}}
		}
	}
	return func() tea.Msg {
		var output, failures []string
		for i, plan := range plans {
			out, err := client.Execute(ctx, plan, dryRun)
			if read {
				output = append(output, out)
			}
			if err != nil {
				name := items[i].Name
				if name == "" {
					name = action.Label
				}
				failures = append(failures, name+"："+err.Error())
			}
		}
		var err error
		if len(failures) > 0 {
			err = fmt.Errorf("操作失败，请检查状态和权限后重试：\n%s", strings.Join(failures, "\n"))
		}
		if !read {
			text := action.Label + "完成"
			if len(items) > 1 {
				text = fmt.Sprintf("%s · %d 项", text, len(items))
			}
			if dryRun {
				text = "演练：" + action.Label + "（未执行修改）"
			}
			if err != nil {
				text = ""
			}
			output = []string{text}
		}
		return resourceDoneMsg{output: strings.Join(output, "\n"), err: err, refresh: action.Mutates && !dryRun, read: read, title: action.Label}
	}
}
func resourceText(out string) string {
	// 外部日志和资源字段不能改变终端状态，换行与制表符用于正常显示。
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(out))
}
func (p *resourcePage) showOutput(title, out string) {
	p.focus = resourceFocusRight
	p.outputTitle = title
	p.outputText = resourceText(out)
	if strings.TrimSpace(p.outputText) == "" {
		p.outputText = "暂无内容"
	}
	p.pane = "output"
	p.output.SetContent(ansi.Hardwrap(p.outputText, max(1, p.output.Width), true))
	p.output.GotoTop()
}

func (p *resourcePage) pullAll() tea.Cmd {
	p.focus = resourceFocusRight
	p.busy = true
	p.mode = resourceList
	p.err = nil
	p.notice = "正在读取本地镜像…"
	client, ctx, dryRun := *p.client, p.ctx, p.env.DryRun
	ch := make(chan tea.Msg, 8)
	p.pullCh = ch
	send := func(msg tea.Msg) {
		select {
		case ch <- msg:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(ch)
		result, err := client.PullAll(ctx, dryRun, func(progress containers.PullProgress) { send(resourcePullMsg(progress)) })
		notice := fmt.Sprintf("拉取完成 · %d/%d 个镜像", result.Succeeded, result.Total)
		if dryRun {
			notice = fmt.Sprintf("演练：将拉取 %d 个镜像（未执行修改）", result.Total)
		}
		if result.Total == 0 && err == nil {
			notice = "没有可拉取的本地镜像标签"
		}
		if result.Skipped > 0 {
			notice += fmt.Sprintf(" · 跳过 %d 个无标签镜像", result.Skipped)
		}
		send(resourceDoneMsg{output: notice, err: err, refresh: !dryRun && result.Total > 0})
	}()
	return p.waitPull()
}
func (p *resourcePage) waitPull() tea.Cmd {
	ch := p.pullCh
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// 方向键只作用于当前面板，切换焦点不会重新查询资源或重置右侧阅读位置。
func (p *resourcePage) navigatePanel(key string) tea.Cmd {
	if p.focus == resourceFocusRight {
		p.pauseLogScroll()
		switch key {
		case "up", "k":
			p.output.LineUp(1)
		case "down", "j":
			p.output.LineDown(1)
		case "pgup":
			p.output.ViewUp()
		case "pgdown":
			p.output.ViewDown()
		case "ctrl+u":
			p.output.LineUp(3)
		case "ctrl+d":
			p.output.LineDown(3)
		case "home":
			p.output.GotoTop()
		case "end":
			p.output.GotoBottom()
		}
		return nil
	}
	old := p.cursor
	switch key {
	case "up", "k":
		p.cursor--
	case "down", "j":
		p.cursor++
	case "pgup":
		p.cursor -= max(1, p.listRows)
	case "pgdown":
		p.cursor += max(1, p.listRows)
	case "ctrl+u":
		p.cursor -= 3
	case "ctrl+d":
		p.cursor += 3
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = len(p.visible()) - 1
	}
	p.cursor = max(0, min(p.cursor, max(0, len(p.visible())-1)))
	if old != p.cursor {
		return p.queueDetails()
	}
	return nil
}
