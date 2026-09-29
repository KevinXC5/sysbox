package screens

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// toneColor 分类色调对应的主题颜色
func toneColor(t cleanup.Tone) lipgloss.AdaptiveColor {
	switch t {
	case cleanup.ToneWarn:
		return theme.Amber
	case cleanup.ToneInfo:
		return theme.Blue
	case cleanup.ToneMuted:
		return theme.Slate
	case cleanup.ToneDanger:
		return theme.Rose
	}
	return theme.Green
}

func (m *cleanPage) color(cat int) lipgloss.AdaptiveColor { return toneColor(m.cats[cat].Tone) }

func (m *cleanPage) Crumbs() []string { return []string{"清理", m.src.Title()} }

func (m *cleanPage) Hints() []string {
	switch m.state {
	case cleanList:
		return []string{"↑↓", "移动", "←→", "分类", "space", "勾选", "a", "全选", "enter", "清理", "esc", "返回", "t", "主题"}
	case cleanBlocked:
		return []string{"r", "重新检测", "esc", "返回列表"}
	case cleanConfirm:
		return []string{"←→", "切换", "enter", "确定", "esc", "取消"}
	case cleanDone:
		return []string{"enter", "返回首页", "r", "重新扫描", "t", "主题"}
	case cleanError:
		return []string{"enter", "返回首页"}
	}
	return []string{"esc", "返回"}
}

func (m *cleanPage) Status() string {
	switch m.state {
	case cleanList:
		return theme.Fg(theme.Accent).Render(fmt.Sprintf("已选 %d 项 · %s", m.selCount(), fsx.FormatBytes(m.selBytes())))
	case cleanChecking:
		return m.spin.View() + " " + theme.SubtleStyle.Render("正在检查…")
	}
	return ""
}

func (m *cleanPage) Body(w, h int) string {
	m.spin.Style = theme.Fg(theme.Accent)
	switch m.state {
	case cleanScanning:
		return m.viewScanning(w, h)
	case cleanList, cleanChecking:
		return m.viewList(w, h)
	case cleanConfirm:
		return widget.Overlay(m.viewList(w, h), m.viewConfirm(w, h), w, h)
	case cleanBlocked:
		return widget.Overlay(m.viewList(w, h), m.viewBlocked(w), w, h)
	case cleanDeleting:
		return m.viewDeleting(w, h)
	case cleanDone:
		return m.viewDone(w, h)
	}
	return widget.Center(w, h, errorBox(w, "无法扫描", fmt.Sprint(m.err)))
}

// columns 条目列表的列宽：名称、所属、路径、大小；越宽显示的列越多，多出的宽度留给路径
func columns(w int, withPath bool) (nameW, groupW, pathW, sizeW int) {
	sizeW = 10
	rest := w - 2 - sizeW // 行首图标占 2 列
	if rest >= 60 {
		groupW = 20
	}
	nameW = rest - groupW
	if withPath && nameW >= 28+1+16 {
		pathW = nameW - 28 - 1
		nameW = 28
	}
	return
}

// itemCols 名称、所属、路径三列
func itemCols(it cleanup.Item, nameW, groupW, pathW int, nameStyle lipgloss.Style) string {
	s := widget.Cell(it.Name, nameW-1, nameStyle, false) + " " + widget.Cell(it.Group, groupW, theme.MutedStyle, false)
	if pathW > 0 {
		s += " " + widget.Cell(fsx.PrettyPath(it.Path), pathW, theme.MutedStyle, false)
	}
	return s
}

// viewScanning 扫描页：满宽进度条，下方逐项显示统计结果
func (m *cleanPage) viewScanning(w, h int) string {
	iw := w - 4
	total := len(m.items)
	done := len(m.measured)
	pct := 0.0
	if total > 0 {
		pct = float64(done) / float64(total)
	}
	title := m.spin.View() + " " + theme.BoldStyle.Render("正在扫描 "+m.src.Title())
	right := theme.MutedStyle.Render(m.stage)
	if total > 0 {
		right = theme.SubtleStyle.Render(fmt.Sprintf("已统计 %d / %d 项", done, total))
	}
	head := widget.ProgressHead(iw, title, right, pct)

	rowsH := max(0, h-lipgloss.Height(head))
	// 可视窗口跟随第一个未完成的条目
	first := total
	for i := range m.items {
		if !m.measured[i] {
			first = i
			break
		}
	}
	start := max(0, min(first-rowsH/2, total-rowsH))
	nameW, groupW, pathW, sizeW := columns(iw, true)
	var rows []string
	for i := start; i < min(total, start+rowsH); i++ {
		it := m.items[i]
		icon, size := theme.MutedStyle.Render("· "), widget.Cell("统计中", sizeW, theme.MutedStyle, true)
		if m.measured[i] {
			icon = theme.Fg(m.color(it.Category)).Render("● ")
			size = widget.Cell(fsx.FormatBytes(it.Size), sizeW, theme.SubtleStyle, true)
		}
		rows = append(rows, icon+itemCols(it, nameW, groupW, pathW, theme.TextStyle)+size)
	}
	return widget.Inset(head + "\n" + strings.Join(rows, "\n"))
}

// viewList 主界面：概览、空间构成、分类标签、列表与详情，各部分按窗口尺寸取舍
func (m *cleanPage) viewList(w, h int) string {
	iw := w - 4
	blocks := []string{""}
	if iw >= 72 && h >= 26 {
		blocks = append(blocks, m.viewCards(iw), "")
	} else {
		blocks = append(blocks, m.viewStatsLine(iw), "")
	}
	if h >= 18 {
		blocks = append(blocks, m.viewComposition(iw), "")
	}
	blocks = append(blocks, m.viewTabs(iw), "")

	listH := max(3, h-lipgloss.Height(strings.Join(blocks, "\n"))-1)
	var main string
	if iw >= 100 {
		lw := iw * 60 / 100
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.viewRows(lw, listH), "  ", m.viewDetail(iw-lw-2, listH))
	} else {
		main = m.viewRows(iw, listH)
	}
	return widget.Inset(strings.Join(append(blocks, main), "\n"))
}

// catSummary 分类卡片上的数值和副标题；主分类显示已勾选的部分
func (m *cleanPage) catSummary(cat int) (value, sub string) {
	n, size, selN, selSize := m.catStats(cat)
	c := m.cats[cat]
	if c.Primary {
		return fsx.FormatBytes(selSize), fmt.Sprintf("已选 %d / %d 项", selN, n)
	}
	sub = fmt.Sprintf("%d 项", n)
	if c.Sub != "" {
		sub += " · " + c.Sub
	}
	return fsx.FormatBytes(size), sub
}

// viewCards 各分类的概览卡片，当前分类的卡片边框高亮
func (m *cleanPage) viewCards(iw int) string {
	const gap = 2
	n := len(m.tabs)
	cw := (iw - gap*(n-1)) / n
	var cards []string
	for i, cat := range m.tabs {
		c := m.cats[cat]
		value, sub := m.catSummary(cat)
		var border lipgloss.TerminalColor = theme.Faint
		if i == m.tab {
			border = m.color(cat)
		}
		// 最后一张卡片吸收整除余数，让右边缘与下方的构成条对齐
		cwi := cw
		if i == n-1 {
			cwi = iw - cw*(n-1) - gap*(n-1)
		}
		inner := cwi - 2 - 4
		content := lipgloss.JoinVertical(lipgloss.Left,
			theme.Fg(m.color(cat)).Render("●")+" "+theme.SubtleStyle.Render(c.Label),
			widget.Cell(value, inner, theme.Fg(m.color(cat)).Bold(true), false),
			widget.Cell(sub, inner, theme.MutedStyle, false),
		)
		card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
			Padding(0, 2).Width(cwi - 2).Render(content)
		if i > 0 {
			cards = append(cards, strings.Repeat(" ", gap))
		}
		cards = append(cards, card)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

// viewStatsLine 窄窗口下代替卡片的单行概览
func (m *cleanPage) viewStatsLine(iw int) string {
	var parts []string
	for _, cat := range m.tabs {
		value, _ := m.catSummary(cat)
		parts = append(parts, theme.Fg(m.color(cat)).Render("●")+" "+
			theme.SubtleStyle.Render(m.cats[cat].Label)+" "+theme.Fg(m.color(cat)).Bold(true).Render(value))
	}
	return theme.Truncate(strings.Join(parts, "   "), iw)
}

// viewComposition 按分类占比绘制的堆叠条，附图例和总量
func (m *cleanPage) viewComposition(iw int) string {
	var total int64
	sizes := make([]int64, len(m.tabs))
	for i, cat := range m.tabs {
		_, sizes[i], _, _ = m.catStats(cat)
		total += sizes[i]
	}
	var legend []string
	for i, cat := range m.tabs {
		pct := 0.0
		if total > 0 {
			pct = float64(sizes[i]) / float64(total) * 100
		}
		legend = append(legend, theme.Fg(m.color(cat)).Render("●")+" "+
			theme.MutedStyle.Render(fmt.Sprintf("%s %.0f%%", m.cats[cat].Label, pct)))
	}
	title := theme.SubtleStyle.Render("空间构成") + "  " + strings.Join(legend, "   ")
	sum := theme.MutedStyle.Render("共 ") + theme.BoldStyle.Render(fsx.FormatBytes(total))
	top := widget.Spread(iw, title, sum)

	// 按比例分配宽度，非零分类至少占 1 格，误差补到最大的一段
	widths := make([]int, len(sizes))
	used, big := 0, 0
	for i, s := range sizes {
		if total > 0 && s > 0 {
			widths[i] = max(1, int(float64(s)/float64(total)*float64(iw)))
		}
		used += widths[i]
		if widths[i] > widths[big] {
			big = i
		}
	}
	if total == 0 {
		return top + "\n" + theme.Rule(iw)
	}
	widths[big] += iw - used
	var bar strings.Builder
	for i, cat := range m.tabs {
		bar.WriteString(theme.Fg(m.color(cat)).Render(strings.Repeat("━", widths[i])))
	}
	return top + "\n" + bar.String()
}

// viewTabs 分类标签，当前分类用彩色胶囊显示
func (m *cleanPage) viewTabs(iw int) string {
	var tabs []string
	for i, cat := range m.tabs {
		n, _, _, _ := m.catStats(cat)
		label := fmt.Sprintf("%s %d", m.cats[cat].Label, n)
		if i == m.tab {
			tabs = append(tabs, theme.Badge(label, m.color(cat)))
		} else {
			tabs = append(tabs, theme.SubtleStyle.Padding(0, 1).Render(label))
		}
	}
	left := strings.Join(tabs, " ")
	idxs := m.tabItems()
	if len(idxs) == 0 {
		return left
	}
	right := theme.MutedStyle.Render(fmt.Sprintf("按大小排序  %d / %d", m.cursor[m.curCat()]+1, len(idxs)))
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > iw {
		return theme.Truncate(left, iw)
	}
	return widget.Spread(iw, left, right)
}

// viewRows 当前分类的条目列表，带勾选状态和相对大小条；窄时依次省略所属列、大小条
func (m *cleanPage) viewRows(lw, h int) string {
	idxs := m.tabItems()
	if len(idxs) == 0 {
		return widget.Center(lw, h, theme.MutedStyle.Render("此分类下没有条目"))
	}
	cat := m.curCat()
	c := m.cursor[cat]
	m.offset[cat] = widget.Scroll(c, m.offset[cat], len(idxs), h)
	off := m.offset[cat]

	var maxSize int64 = 1
	for _, i := range idxs {
		maxSize = max(maxSize, m.items[i].Size)
	}
	color := m.color(cat)

	const sizeW = 9
	groupW, barW := 18, 10
	if lw < 70 {
		groupW = 0
	}
	if lw < 50 {
		barW = 0
	}
	nameW := lw - 4 - sizeW - 1 // 4 列留给光标和勾选，1 列是大小前的空格
	if groupW > 0 {
		nameW -= groupW + 1
	}
	if barW > 0 {
		nameW -= barW + 1
	}

	var rows []string
	for r := off; r < min(len(idxs), off+h); r++ {
		i := idxs[r]
		it := m.items[i]
		cur := r == c

		mark := "  "
		if cur {
			mark = theme.Fg(theme.Accent).Render("▌ ")
		}
		check := theme.FaintStyle.Render("· ")
		if it.Selectable {
			if m.selected[i] {
				check = theme.Fg(color).Render("◉ ")
			} else {
				check = theme.MutedStyle.Render("○ ")
			}
		}
		nameStyle, sizeStyle := theme.TextStyle, theme.SubtleStyle
		if it.Selectable && !m.selected[i] {
			nameStyle = theme.SubtleStyle
		}
		if cur {
			nameStyle, sizeStyle = theme.Fg(theme.Accent).Bold(true), theme.BoldStyle
		}

		row := mark + check + widget.Cell(it.Name, nameW, nameStyle, false)
		if groupW > 0 {
			row += " " + widget.Cell(it.Group, groupW, theme.MutedStyle, false)
		}
		if barW > 0 {
			filled := int(float64(it.Size) / float64(maxSize) * float64(barW))
			if it.Size > 0 && filled == 0 {
				filled = 1
			}
			row += " " + theme.Fg(color).Render(strings.Repeat("━", filled)) +
				theme.FaintStyle.Render(strings.Repeat("━", barW-filled))
		}
		rows = append(rows, row+" "+widget.Cell(fsx.FormatBytes(it.Size), sizeW, sizeStyle, true))
	}
	return lipgloss.NewStyle().Width(lw).Height(h).Render(strings.Join(rows, "\n"))
}

// viewDetail 右侧详情面板，展示当前条目的完整信息
func (m *cleanPage) viewDetail(dw, h int) string {
	idxs := m.tabItems()
	if len(idxs) == 0 {
		return widget.Panel(dw, h, theme.Faint, theme.MutedStyle.Render("没有可查看的条目"))
	}
	i := idxs[m.cursor[m.curCat()]]
	it := m.items[i]
	inner := widget.PanelInner(dw)
	kind := "目录"
	if !it.IsDir {
		kind = "文件"
	}
	wrap := func(s string, st lipgloss.Style) string { return st.Width(inner).Render(s) }
	lines := []string{
		theme.BoldStyle.Render(it.Name),
		theme.Badge(m.cats[it.Category].Label, m.color(it.Category)) + "  " + theme.SubtleStyle.Render(kind),
		"",
		widget.KV("大小", 6, theme.BoldStyle.Render(fsx.FormatBytes(it.Size))),
		widget.KV("所属", 6, theme.TextStyle.Render(it.Group)),
		"",
		widget.Section("位置"),
		wrap(widget.WrapPath(fsx.PrettyPath(it.Path), inner), theme.SubtleStyle),
		"",
		widget.Section("说明"),
		wrap(it.Note, theme.TextStyle),
		"",
		wrap(m.detailStatus(i), lipgloss.NewStyle()),
	}
	return widget.Panel(dw, h, theme.Faint, strings.Join(lines, "\n"))
}

// detailStatus 当前条目将如何处理
func (m *cleanPage) detailStatus(i int) string {
	it := m.items[i]
	switch {
	case it.Irreversible && m.selected[i]:
		return theme.Fg(theme.Rose).Render("⚠ 已纳入清理，删除后无法恢复")
	case it.Irreversible:
		return theme.Fg(theme.Amber).Render("◌ 默认不删，按空格可纳入清理")
	case it.Selectable && m.selected[i]:
		return theme.Fg(theme.Green).Render("✓ 将被清理")
	case it.Selectable:
		return theme.MutedStyle.Render("○ 未勾选，按空格纳入清理")
	}
	return theme.Fg(m.color(it.Category)).Render("· 只展示，不会删除")
}

// viewConfirm 确认弹窗；窗口矮时减少列出的条目数
func (m *cleanPage) viewConfirm(bw, bh int) string {
	w := widget.DialogWidth(64, bw)
	inner := widget.DialogInner(w)
	var accent lipgloss.TerminalColor = theme.Rose
	action := "开始清理"
	if m.dryRun {
		accent, action = theme.Accent, "开始演练"
	}
	title := theme.BoldStyle.Render("确认清理")
	summary := theme.SubtleStyle.Render("将删除 ") + theme.Fg(accent).Bold(true).Render(fmt.Sprint(m.selCount())) +
		theme.SubtleStyle.Render(" 项，预计释放 ") + theme.Fg(accent).Bold(true).Render(fsx.FormatBytes(m.selBytes()))

	var notes []string
	for _, n := range m.src.Notes() {
		notes = append(notes, theme.MutedStyle.Render("· "+n))
	}
	if m.notice != nil {
		notes = append(notes, theme.Fg(theme.Amber).Render("⚠ "+m.notice.Title+"："+strings.Join(m.notice.Lines, "、")))
	}
	if m.selectedIrreversible() {
		notes = append(notes, theme.Fg(theme.Rose).Render("⚠ 包含删除后无法恢复的条目"))
	}
	if m.dryRun {
		notes = append(notes, theme.Fg(theme.Amber).Render("◌ 演练模式：只模拟流程，不会删除任何文件"))
	}
	for i := range notes {
		notes[i] = theme.Truncate(notes[i], inner)
	}

	// 列出最大的几项，数量受窗口高度限制
	var idxs []int
	for i := range m.items {
		if m.selected[i] {
			idxs = append(idxs, i)
		}
	}
	sort.Slice(idxs, func(a, b int) bool { return m.items[idxs[a]].Size > m.items[idxs[b]].Size })
	maxItems := min(5, max(0, bh-14-len(notes)))
	var list []string
	groupW := max(0, inner-2-22-10)
	for _, i := range idxs[:min(maxItems, len(idxs))] {
		it := m.items[i]
		list = append(list, theme.FaintStyle.Render("│ ")+
			widget.Cell(it.Name, 22, theme.TextStyle, false)+
			widget.Cell(it.Group, groupW, theme.MutedStyle, false)+
			widget.Cell(fsx.FormatBytes(it.Size), 10, theme.SubtleStyle, true))
	}
	if rest := len(idxs) - len(list); rest > 0 && maxItems > 0 {
		list = append(list, theme.FaintStyle.Render("│ ")+theme.MutedStyle.Render(fmt.Sprintf("… 另外 %d 项", rest)))
	}

	parts := []string{title, "", summary, ""}
	if len(list) > 0 {
		parts = append(parts, strings.Join(list, "\n"), "")
	}
	parts = append(parts, strings.Join(notes, "\n"), "",
		widget.Buttons(inner, widget.Button("取消", !m.focusOK, theme.Subtle), widget.Button(action, m.focusOK, accent)))
	return widget.Dialog(accent, w, strings.Join(parts, "\n"))
}

// viewBlocked 检查未通过时的阻止弹窗
func (m *cleanPage) viewBlocked(bw int) string {
	w := widget.DialogWidth(72, bw)
	inner := widget.DialogInner(w)
	var lines []string
	for _, l := range m.notice.Lines {
		lines = append(lines, theme.Truncate(theme.SubtleStyle.Render(l), inner))
	}
	return widget.Dialog(theme.Rose, w,
		theme.Fg(theme.Rose).Bold(true).Render("⚠ "+m.notice.Title)+"\n\n"+strings.Join(lines, "\n"))
}

// viewDeleting 执行页：满宽进度条，下方滚动显示删除记录，行数随窗口高度增减
func (m *cleanPage) viewDeleting(w, h int) string {
	iw := w - 4
	// 进度按项数计算；按字节算时大项排在前面，进度条会过早跑满
	pct := 0.0
	if len(m.queue) > 0 {
		pct = float64(m.pos) / float64(len(m.queue))
	}
	verb := "正在清理"
	if m.dryRun {
		verb = "正在演练"
	}
	title := m.spin.View() + " " + theme.BoldStyle.Render(verb) + "  " +
		theme.SubtleStyle.Render(fmt.Sprintf("%d / %d", m.pos, len(m.queue)))
	amount := theme.MutedStyle.Render("已释放 ") + theme.Fg(theme.Accent2).Bold(true).Render(fsx.FormatBytes(m.freed)) +
		theme.MutedStyle.Render(" / "+fsx.FormatBytes(m.target))
	head := widget.ProgressHead(iw, title, amount, pct)

	rowsH := max(1, h-lipgloss.Height(head))
	nameW, groupW, pathW, sizeW := columns(iw, true)
	hasNext := m.pos < len(m.queue)
	keep := rowsH
	if hasNext {
		keep--
	}
	var rows []string
	for _, l := range m.logs[max(0, len(m.logs)-keep):] {
		icon, st := theme.Fg(theme.Green).Render("✓ "), theme.SubtleStyle
		if l.err != nil {
			icon, st = theme.Fg(theme.Rose).Render("✗ "), theme.Fg(theme.Rose)
		}
		rows = append(rows, icon+itemCols(l.item, nameW, groupW, pathW, st)+
			widget.Cell(fsx.FormatBytes(l.item.Size), sizeW, theme.MutedStyle, true))
	}
	if hasNext {
		next := m.items[m.queue[m.pos]]
		rows = append(rows, theme.Fg(theme.Accent).Render("› ")+
			itemCols(next, nameW, groupW, pathW, theme.BoldStyle)+
			widget.Cell(fsx.FormatBytes(next.Size), sizeW, theme.SubtleStyle, true))
	}
	return widget.Inset(head + "\n" + strings.Join(rows, "\n"))
}

// viewDone 完成页：满屏面板，内容居中；空间不足时逐级收紧排版
func (m *cleanPage) viewDone(w, h int) string {
	iw, ph := w-4, h-2
	title, caption := "清理完成", "已释放空间"
	if m.dryRun {
		title, caption = "演练完成", "预计可释放"
	}
	check := lipgloss.NewStyle().Foreground(theme.OnColor).Background(theme.Green).Bold(true).Padding(0, 1).Render("✓")
	head := check + "  " + theme.BoldStyle.Render(title)

	var hero string
	if ph >= 26 && iw >= 50 {
		num, unit := fsx.SplitBytes(m.freed)
		big := lipgloss.JoinHorizontal(lipgloss.Bottom, theme.BigNumber(num), "  ", theme.Fg(theme.Accent2).Bold(true).Render(unit))
		hero = lipgloss.JoinVertical(lipgloss.Center, big, "", theme.SubtleStyle.Render(caption))
	} else {
		hero = theme.Fg(theme.Accent2).Bold(true).Render(fsx.FormatBytes(m.freed)) + "  " + theme.SubtleStyle.Render(caption)
	}

	stat := func(k, v string, c lipgloss.TerminalColor) string {
		return theme.MutedStyle.Render(k+"  ") + theme.Fg(c).Bold(true).Render(v)
	}
	var failColor lipgloss.TerminalColor = theme.Subtle
	if m.failed > 0 {
		failColor = theme.Rose
	}
	stats := stat("删除", fmt.Sprintf("%d 项", len(m.queue)-m.failed), theme.Green) + "     " +
		stat("失败", fmt.Sprintf("%d 项", m.failed), failColor) + "     " +
		stat("用时", fmt.Sprintf("%.1f 秒", m.elapsed.Seconds()), theme.Text)

	changes := m.viewRootChanges()
	failures := m.viewFailures(min(56, iw-8))
	ruleW := min(56, iw-8)
	tip := theme.MutedStyle.Render(m.src.Tip())

	// 由宽松到紧凑依次尝试，取第一个放得下的排版，避免内容超出面板被裁掉
	avail := ph - 4
	var layouts [][]string
	full := []string{head, "", hero, "", theme.Rule(ruleW), "", stats}
	for _, extra := range []string{changes, failures} {
		if extra != "" {
			full = append(full, "", extra)
		}
	}
	layouts = append(layouts, append(full, "", theme.Rule(ruleW), "", tip))
	layouts = append(layouts, []string{head, "", hero, "", stats, "", changes, failures})
	layouts = append(layouts, []string{head, hero, stats, changes})
	layouts = append(layouts, []string{head, hero, stats})
	var content string
	for _, parts := range layouts {
		content = lipgloss.JoinVertical(lipgloss.Center, nonEmpty(parts)...)
		if lipgloss.Height(content) <= avail {
			break
		}
	}
	box := widget.Panel(iw, ph, theme.Faint, widget.Center(iw-6, avail, content))
	return "\n" + widget.Inset(box)
}

// nonEmpty 去掉由可选区块留下的连续空行
func nonEmpty(parts []string) []string {
	var out []string
	for i, p := range parts {
		if p == "" && (i == 0 || parts[i-1] == "") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// viewRootChanges 各根目录清理前后的大小对比
func (m *cleanPage) viewRootChanges() string {
	roots := m.src.Roots()
	if len(roots) == 0 || len(m.before) < len(roots) {
		return ""
	}
	arrow := theme.MutedStyle.Render("  →  ")
	var lines []string
	for i, r := range roots {
		line := widget.Cell(r.Label, 22, theme.SubtleStyle, false) + widget.Cell(fsx.FormatBytes(m.before[i]), 10, theme.MutedStyle, true)
		if r.Untouched || i >= len(m.after) {
			line += theme.MutedStyle.Render("     未改动")
		} else {
			line += arrow + widget.Cell(fsx.FormatBytes(m.after[i]), 10, theme.BoldStyle, false)
		}
		lines = append(lines, line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// viewFailures 失败条目及原因，最多列 5 条
func (m *cleanPage) viewFailures(w int) string {
	var lines []string
	for _, l := range m.logs {
		if l.err != nil && len(lines) < 5 {
			lines = append(lines, theme.Truncate(theme.Fg(theme.Rose).Render("✗ "+l.item.Name+"  ")+theme.MutedStyle.Render(l.err.Error()), w))
		}
	}
	return strings.Join(lines, "\n")
}
