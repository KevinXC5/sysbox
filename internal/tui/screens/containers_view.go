package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	"github.com/KevinXC5/sysbox/internal/ui/search"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

type resourceShortcut struct{ key, id, label string }

// 快捷键和提示共用一份映射，提示只包含当前资源状态支持的操作。
func (p *resourcePage) shortcuts() []resourceShortcut {
	var shortcuts []resourceShortcut
	item, ok := p.current()
	if !p.client.Kubernetes {
		shortcuts = append(shortcuts, resourceShortcut{"p", "pull", "拉取镜像"}, resourceShortcut{"P", "pull-all", "拉取所有镜像"})
		if ok && p.kinds[p.tab] == "images" {
			shortcuts = append(shortcuts, resourceShortcut{"s", "run", "创建容器"}, resourceShortcut{"g", "tag", "添加标签"}, resourceShortcut{"d", "rmi", "删除"})
		} else if ok {
			shortcuts = append(shortcuts, resourceShortcut{"l", "logs", "实时日志"}, resourceShortcut{"e", "exec", "终端"})
			switch item.State {
			case "running":
				shortcuts = append(shortcuts, resourceShortcut{"s", "stop", "停止"}, resourceShortcut{"R", "restart", "重启"}, resourceShortcut{"a", "pause", "暂停"})
			case "paused":
				shortcuts = append(shortcuts, resourceShortcut{"s", "unpause", "恢复"})
			default:
				shortcuts = append(shortcuts, resourceShortcut{"s", "start", "启动"})
			}
			shortcuts = append(shortcuts, resourceShortcut{"d", "rm", "删除"})
		}
	} else if ok {
		switch p.kinds[p.tab] {
		case "deployments":
			shortcuts = append(shortcuts, resourceShortcut{"l", "logs", "实时日志"}, resourceShortcut{"s", "scale", "扩缩容"}, resourceShortcut{"R", "restart", "滚动重启"}, resourceShortcut{"u", "rollout", "发布状态"}, resourceShortcut{"e", "edit", "编辑 YAML"})
		case "configmaps":
			shortcuts = append(shortcuts, resourceShortcut{"e", "edit", "编辑 YAML"})
		case "pods":
			shortcuts = append(shortcuts, resourceShortcut{"l", "logs", "实时日志"}, resourceShortcut{"e", "exec", "终端"})
		case "nodes":
			shortcut := resourceShortcut{"s", "cordon", "禁止调度"}
			if item.State == "cordoned" {
				shortcut = resourceShortcut{"s", "uncordon", "恢复调度"}
			}
			shortcuts = append(shortcuts, shortcut, resourceShortcut{"D", "drain", "排空节点"})
		}
		shortcuts = append(shortcuts, resourceShortcut{"v", "yaml", "YAML"}, resourceShortcut{"i", "describe", "详细状态"})
		if p.kinds[p.tab] != "nodes" {
			shortcuts = append(shortcuts, resourceShortcut{"d", "delete", "删除"})
		}
	}
	var available []resourceShortcut
	for _, shortcut := range shortcuts {
		if shortcut.id == "pull" || shortcut.id == "pull-all" {
			available = append(available, shortcut)
			continue
		}
		if _, _, ok := p.findAction(shortcut.id); ok {
			available = append(available, shortcut)
		}
	}
	return available
}
func (p *resourcePage) quickHints(w int) string {
	var lines []string
	line := ""
	add := func(key, label string) {
		hint := theme.Key(key) + " " + theme.SubtleStyle.Render(label)
		if lipgloss.Width(line)+lipgloss.Width(hint)+3 > w && line != "" {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += hint
	}
	add("/", "模糊查询")
	add("f", "详情查询")
	for _, shortcut := range p.shortcuts() {
		add(shortcut.key, shortcut.label)
	}
	if p.pane == "logs" {
		label := "暂停自动滚动"
		if !p.logAutoScroll {
			label = "恢复自动滚动"
		}
		add("space", label)
	}
	if p.pane != "info" {
		add("b", "基础信息")
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return theme.MutedStyle.Render("选择资源后可使用快捷键")
	}
	return strings.Join(lines, "\n")
}
func (p *resourcePage) Body(w, h int) string {
	if w < 36 || h < 12 {
		return theme.MutedStyle.Render("请扩大终端窗口（至少 36 列、12 行）")
	}
	iw := w - 4
	target := p.client.Target
	if target == "" {
		target = "环境变量 / 默认连接"
	}
	head := theme.BoldStyle.Render("环境：" + resourceText(target))
	if p.client.Kubernetes {
		head += "  " + theme.SubtleStyle.Render("命名空间："+resourceText(p.namespace))
	}
	spacing := "\n\n"
	if h < 22 {
		spacing = "\n"
	}
	top := theme.Truncate(head, iw) + "\n" + p.resourceTabs(iw, h) + spacing + p.quickHints(iw)

	top = lipgloss.NewStyle().MaxWidth(iw).Render(top)
	room := max(5, h-lipgloss.Height(top)-1)
	// 列表占五分之二，信息表占五分之三，给路径和运行配置留出更多宽度。
	gap := 2
	lw := (iw - gap) * 2 / 5
	rw := iw - gap - lw
	left := p.listPane(lw, room)
	right := p.detailPane(rw, room)
	return widget.Inset(top + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right))
}

// 标签用完整底色区分选中状态，和首页分类保持一致；切换提示紧邻标签。
func (p *resourcePage) resourceTabs(w, h int) string {
	var tabs []string
	for i, label := range p.labels {
		style := lipgloss.NewStyle().Foreground(theme.Muted).Background(theme.Surface).Padding(0, 2)
		if i == p.tab {
			style = style.Foreground(theme.OnColor).Background(theme.Accent).Bold(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, interleaveTabs(tabs)...)
	hint := theme.Key("Tab") + theme.MutedStyle.Render(" 切换资源")
	if lipgloss.Width(bar)+3+lipgloss.Width(hint) <= w {
		return bar + "   " + hint
	}
	return bar + "\n" + theme.Truncate(hint, w)
}

func interleaveTabs(tabs []string) []string {
	var parts []string
	for i, tab := range tabs {
		if i > 0 {
			parts = append(parts, " ")
		}
		parts = append(parts, tab)
	}
	return parts
}

func (p *resourcePage) listPane(w, h int) string {
	inner := max(1, widget.PanelInner(w))
	title := p.labels[p.tab] + fmt.Sprintf(" · %d/%d", len(p.visible()), len(p.items))
	statusLabel := "状态"
	if p.kinds[p.tab] == "images" {
		statusLabel = "大小"
	}
	if p.kinds[p.tab] == "deployments" {
		statusLabel = "就绪"
	}
	if p.kinds[p.tab] == "configmaps" {
		statusLabel = "数据"
	}
	statusW := min(24, max(8, inner*30/100))
	if inner < 30 {
		statusW = 0
	}
	nameW := inner - statusW - 4
	header := widget.Cell("名称", max(1, nameW+4), theme.MutedStyle, false) + widget.Cell(statusLabel, statusW, theme.MutedStyle, false)
	lines := []string{theme.BoldStyle.Render(title)}
	if p.mode == resourceInput && p.inputPurpose == "filter" {
		p.input.Width = max(1, inner-2)
		p.input.TextStyle = theme.TextStyle
		p.input.PromptStyle = theme.Fg(theme.Accent)
		lines = append(lines, p.input.View(), theme.Truncate(theme.MutedStyle.Render("↑↓ 选择 · enter 完成 · esc 清空"), inner))
	} else {
		hint := theme.Key("/") + " " + theme.MutedStyle.Render("模糊查询名称 / ID / 镜像")
		if p.filter != "" {
			hint = theme.Key("/") + " " + theme.SubtleStyle.Render(p.filter)
		}
		lines = append(lines, theme.Truncate(hint, inner))
	}
	lines = append(lines, header, theme.Rule(inner))
	items := p.visible()
	p.cursor = min(p.cursor, max(0, len(items)-1))
	visibleRows := max(1, h-4-len(lines))
	p.listRows = visibleRows
	p.offset = widget.Scroll(p.cursor, p.offset, len(items), visibleRows)
	for i := p.offset; i < min(len(items), p.offset+visibleRows); i++ {
		item := items[i]
		name := resourceText(item.Name)
		if p.client.Kubernetes && p.namespace == "*" && item.Namespace != "" {
			name = resourceText(item.Namespace) + "/" + name
		}
		mark, check := " ", " "
		if p.selected[itemKey(item)] {
			check = "✓"
		}
		style := theme.TextStyle
		if i == p.cursor {
			mark = "▌"
			style = theme.Fg(theme.Accent).Bold(true)
		}
		status := ""
		if len(item.Columns) > 0 {
			status = item.Columns[0]
		}
		if p.kinds[p.tab] == "containers" && len(item.Columns) > 1 {
			status = item.Columns[1]
		}
		row := mark + check + "  " + widget.Cell(name, max(1, nameW), style, false) + widget.Cell(resourceText(status), statusW, theme.SubtleStyle, false)
		if i == p.cursor {
			row = lipgloss.NewStyle().Background(theme.Surface).Width(inner).Render(row)
		}
		lines = append(lines, theme.Truncate(row, inner))
	}
	if len(items) == 0 {
		text := "暂无资源"
		if p.busy {
			text = "正在读取…"
		}
		lines = append(lines, theme.MutedStyle.Render(text))
	}
	return widget.Panel(w, h, p.panelBorder(resourceFocusLeft), strings.Join(lines, "\n"))
}
func (p *resourcePage) baseFields() []containers.Field {
	item, ok := p.current()
	if !ok {
		return nil
	}
	if fields, ok := p.details[itemKey(item)]; ok {
		return fields
	}
	if len(item.Fields) > 0 {
		return item.Fields
	}
	fields := []containers.Field{{Label: "名称", Value: item.Name}}
	if item.Namespace != "" {
		fields = append(fields, containers.Field{Label: "命名空间", Value: item.Namespace})
	}
	labels := []string{"状态", "信息", "位置"}
	if p.kinds[p.tab] == "containers" {
		labels = []string{"镜像", "状态", "端口"}
	}
	if p.kinds[p.tab] == "images" {
		labels = []string{"大小", "创建时间", "镜像 ID"}
	}
	for i, value := range item.Columns {
		if i < len(labels) {
			fields = append(fields, containers.Field{Label: labels[i], Value: value})
		}
	}
	return fields
}
func fieldTable(fields []containers.Field, w int) string {
	labelW := min(12, max(6, w/3))
	valueW := max(1, w-labelW-3)
	lines := []string{widget.Cell("字段", labelW, theme.MutedStyle, false) + theme.FaintStyle.Render(" │ ") + theme.MutedStyle.Render("内容"), theme.Rule(w)}
	for _, field := range fields {
		if field.Label == "" {
			title := theme.Truncate(theme.Fg(theme.Accent).Bold(true).Render(resourceText(field.Value)), w)
			lines = append(lines, "", title+" "+theme.Rule(max(0, w-lipgloss.Width(title)-1)))
			continue
		}
		value := strings.ReplaceAll(resourceText(field.Value), "\t", "    ")
		if strings.TrimSpace(value) == "" || value == "<none>" {
			value = "—"
		}
		var wrapped []string
		for _, line := range strings.Split(value, "\n") {
			wrapped = append(wrapped, wrapText(line, valueW)...)
		}
		for i, line := range wrapped {
			label := ""
			if i == 0 {
				label = resourceText(field.Label)
			}
			lines = append(lines, widget.Cell(label, labelW, theme.MutedStyle, false)+theme.FaintStyle.Render(" │ ")+theme.TextStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}
func (p *resourcePage) detailPane(w, h int) string {
	inner := max(1, widget.PanelInner(w))
	var top []string
	if p.busy {
		top = append(top, theme.Fg(theme.Accent).Render("正在执行，请稍候…"))
	}
	if p.notice != "" {
		top = append(top, theme.Truncate(theme.Fg(theme.Green).Render(resourceText(p.notice)), inner))
	}
	if p.env.DryRun {
		top = append(top, theme.Fg(theme.Amber).Render("演练模式 · 修改不会执行"))
	}
	switch p.mode {
	case resourceInput:
		if p.inputPurpose == "filter" {
			break
		}
		title := p.action.Prompt
		if p.inputPurpose == "detail" {
			title = "模糊查询右侧字段 / 内容"
		}
		if p.inputPurpose == "namespace" {
			title = "命名空间，* 表示全部"
		}
		p.input.Width = max(1, inner-2)
		p.input.TextStyle = theme.TextStyle
		p.input.PromptStyle = theme.Fg(theme.Accent)
		top = append(top, theme.Truncate(theme.BoldStyle.Render(title), inner), p.input.View(), theme.MutedStyle.Render("enter 确定 · esc 取消"))
	case resourceConfirm:
		top = append(top, theme.Fg(theme.Amber).Bold(true).Render("确认"+p.action.Label), theme.Truncate(theme.SubtleStyle.Render("环境："+p.client.Target), inner))
		if len(p.actionItems) > 1 {
			top = append(top, theme.Truncate(theme.Fg(theme.Amber).Render(fmt.Sprintf("共 %d 项（含筛选隐藏的已选项）", len(p.actionItems))), inner))
		}

		if p.action.Note != "" && h >= 22 {
			top = append(top, theme.Truncate(theme.Fg(theme.Amber).Render(p.action.Note), inner))
		}
		top = append(top, theme.Fg(theme.Amber).Render("y 确认 · esc 取消"))
	}
	if p.err != nil {
		lines := wrapText(resourceText(p.err.Error()), inner)
		limit := max(1, min(4, h/4))
		for _, line := range lines[:min(len(lines), limit)] {
			top = append(top, theme.Fg(theme.Rose).Render(line))
		}
	}
	if p.detailQuery != "" && (p.mode != resourceInput || p.inputPurpose != "detail") {
		top = append(top, theme.Truncate(theme.SubtleStyle.Render("查询："+resourceText(p.detailQuery)), inner))
	}
	if p.mode == resourcePicker {
		return p.pickerPane(w, h, top)
	}
	title := "基础信息"
	if p.mode == resourceConfirm {
		title = "操作目标"
	}
	if p.pane != "info" {
		title = p.outputTitle + " · b 基础信息"
	}
	if p.detailPending && p.pane == "info" {
		title += " · 补充中…"
	}
	top = append(top, theme.Truncate(theme.BoldStyle.Render(title), inner))
	if p.pane == "logs" {
		state := theme.Fg(theme.Green).Render("● 自动滚动：开")
		if !p.logAutoScroll {
			state = theme.Fg(theme.Amber).Render("◌ 自动滚动：暂停")
		}
		top = append(top, theme.Truncate(theme.Key("space")+" "+state, inner))
		if !p.logAlive {
			top = append(top, theme.Truncate(theme.MutedStyle.Render("日志流已结束 · l 重新连接"), inner))
		} else if !p.logAutoScroll {
			top = append(top, theme.Truncate(theme.MutedStyle.Render(fmt.Sprintf("继续接收 · %d 条新日志", p.logReceived-p.logPauseAt)), inner))
		}
		if p.logError != nil {
			top = append(top, theme.Truncate(theme.Fg(theme.Rose).Render(resourceText(p.logError.Error())), inner))
		}
	}
	content := p.outputText
	if p.mode == resourceConfirm {
		var targets []containers.Field
		for _, item := range p.actionItems {
			name := item.Name
			if item.Namespace != "" {
				name = item.Namespace + "/" + name
			}
			targets = append(targets, containers.Field{Label: p.labels[p.tab], Value: name})
		}
		content = fieldTable(targets, inner)
	} else if p.pane == "info" {
		fields := p.baseFields()
		if p.detailQuery != "" {
			fields = filterResourceFields(fields, p.detailQuery)
		}
		if len(fields) == 0 {
			content = "选择左侧资源查看基础信息"
			if p.detailQuery != "" {
				content = "未找到匹配的字段或内容"
			}
		} else {
			content = fieldTable(fields, inner)
		}
		if p.detailError != nil {
			content += "\n\n" + theme.Fg(theme.Amber).Render("补充信息读取失败，按 r 重试") + "\n" + ansi.Hardwrap(resourceText(p.detailError.Error()), inner, true)
		}
	} else {
		if p.detailQuery != "" {
			var matches []string
			for _, line := range strings.Split(content, "\n") {
				if _, ok := search.Score(p.detailQuery, line); ok {
					matches = append(matches, line)
				}
			}
			content = strings.Join(matches, "\n")
			if len(matches) == 0 {
				content = "未找到匹配内容"
			}
		}
		content = ansi.Hardwrap(content, inner, true)
	}
	height := max(1, h-5-len(top))
	// 内容随窗口折行，保留滚动位置；左右列表与右侧阅读互不切换页面。
	p.output.Width, p.output.Height = inner, height
	offset := p.output.YOffset
	p.output.SetContent(content)
	p.output.SetYOffset(offset)
	if p.pane == "logs" && p.logAutoScroll {
		p.output.GotoBottom()
	}
	top = append(top, p.output.View())
	total := strings.Count(content, "\n") + 1
	position := fmt.Sprintf("%d–%d / %d 行", p.output.YOffset+1, min(total, p.output.YOffset+height), total)
	top = append(top, theme.Truncate(theme.MutedStyle.Render("↑↓ 滚动 · PgUp/PgDn 翻页 · "+position), inner))
	return widget.Panel(w, h, p.panelBorder(resourceFocusRight), strings.Join(top, "\n"))
}
func (p *resourcePage) pickerPane(w, h int, top []string) string {
	inner := max(1, widget.PanelInner(w))
	title := "切换环境"
	if p.picker == "namespace" {
		title = "选择命名空间"
	}
	if p.picker == "container" {
		title = "选择容器并进入终端"
	}
	p.input.Width = max(1, inner-2)
	p.input.TextStyle = theme.TextStyle
	p.input.PromptStyle = theme.Fg(theme.Accent)
	values := p.visibleChoices()
	top = append(top, theme.Truncate(theme.BoldStyle.Render(title), inner), p.input.View(), theme.Truncate(theme.MutedStyle.Render("输入模糊查询 · ↑↓ 选择 · enter 确定"), inner), theme.Rule(inner))
	room := max(1, h-4-len(top))
	start := widget.Scroll(p.choice, 0, len(values), room)
	for i := start; i < min(len(values), start+room); i++ {
		name := values[i]
		if name == "" {
			name = "环境变量 / 默认连接"
		}
		if name == "*" {
			name = "全部命名空间"
		}
		mark := "  "
		style := theme.TextStyle
		if i == p.choice {
			mark = "▌ "
			style = theme.Fg(theme.Accent).Bold(true)
		}
		top = append(top, mark+widget.Cell(resourceText(name), max(1, inner-2), style, false))
	}
	if len(values) == 0 {
		top = append(top, theme.MutedStyle.Render("暂无选项，按 esc 返回"))
	}
	return widget.Panel(w, h, p.panelBorder(resourceFocusRight), strings.Join(top, "\n"))
}

// 详情保持原有分组和顺序，只保留命中的行；可通过组名一次定位全部挂载或网络信息。
func filterResourceFields(fields []containers.Field, query string) []containers.Field {
	var matches []containers.Field
	group, lastGroup := "", ""
	for _, field := range fields {
		if field.Label == "" {
			group = field.Value
			continue
		}
		if _, ok := search.Score(query, field.Label, field.Value, group); !ok {
			continue
		}
		if group != "" && group != lastGroup {
			matches = append(matches, containers.Field{Value: group})
			lastGroup = group
		}
		matches = append(matches, field)
	}
	return matches
}

func (p *resourcePage) panelBorder(focus resourceFocus) lipgloss.TerminalColor {
	if p.focus == focus {
		return theme.Accent
	}
	return theme.Faint
}
