package screens

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// SelfUpdateID 首页上触发自升级时打开的页面
const SelfUpdateID = "self-update"

// sysInfo 首页底部的系统概况
type sysInfo struct {
	os          string
	free, total int64
}

type sysInfoMsg sysInfo

// Home 首页：顶部是分类方块，用 ←→ 切换；下方左侧列出当前分类的条目，用 ↑↓ 选择，右侧是详情
type Home struct {
	tools      []Tool
	env        Env
	cats       []category
	cat        int // 当前分类
	info       sysInfo
	newVersion string
	sums       map[string]*toolSum // 按 s 扫描后各清理工具的可释放空间
	gen        int
	input      textinput.Model // 编辑文字型设置
	editErr    string          // 输入不合法时的提示
}

// category 一个分类，pos 记住最后选中的条目，切回来时保持原位
type category struct {
	Group
	items []item
	pos   int
}

// item 分类里的一个条目：工具或设置，二选一
type item struct {
	tool *Tool
	set  *Setting
}

func (it item) name() string {
	if it.tool != nil {
		return it.tool.Name
	}
	return it.set.Name
}

func (it item) icon() []string {
	if it.tool != nil {
		return it.tool.Icon
	}
	return it.set.Icon
}

func (it item) desc() string {
	if it.tool != nil {
		return it.tool.Desc
	}
	return it.set.Desc
}

// NewHome 创建首页。groups 固定列出的分类，还没有条目的分类也显示方块；
// 工具和设置按各自的分组归类，不在 groups 里的分组按出现顺序追加在后面
func NewHome(groups []Group, tools []Tool, settings []Setting, env Env) *Home {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 400
	m := &Home{tools: tools, env: env, input: in}
	for _, g := range groups {
		m.cats = append(m.cats, category{Group: g})
	}
	add := func(group string, it item) {
		j := slices.IndexFunc(m.cats, func(c category) bool { return c.Name == group })
		if j < 0 {
			m.cats = append(m.cats, category{Group: Group{Name: group}})
			j = len(m.cats) - 1
		}
		m.cats[j].items = append(m.cats[j].items, it)
	}
	for i := range m.tools {
		add(m.tools[i].Group, item{tool: &m.tools[i]})
	}
	for i := range settings {
		add(settings[i].Group, item{set: &settings[i]})
	}
	if len(m.cats) == 0 {
		m.cats = []category{{}}
	}
	return m
}

// current 当前选中的条目，分类里还没有条目时返回 false
func (m *Home) current() (item, bool) {
	c := m.cats[m.cat]
	if len(c.items) == 0 {
		return item{}, false
	}
	return c.items[c.pos], true
}

// SetNewVersion 有可用新版本时由根模型设置
func (m *Home) SetNewVersion(v string) { m.newVersion = v }

func (m *Home) Init() tea.Cmd { return loadSysInfo }

// loadSysInfo 读取系统版本与数据盘空间，实现按平台区分
func loadSysInfo() tea.Msg { return sysInfoMsg(readSysInfo()) }

func (m *Home) Busy() bool { return false }

// Typing 编辑设置时单字母按键交给输入框
func (m *Home) Typing() bool { return m.input.Focused() }

func (m *Home) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sysInfoMsg:
		m.info = sysInfo(msg)
	case homeSumMsg:
		if sum := m.sums[msg.id]; sum != nil && sum.gen == msg.gen {
			sum.cancel()
			*sum = toolSum{gen: msg.gen, done: true, n: msg.n, size: msg.size, partial: msg.partial}
			if msg.err != nil {
				sum.failure = msg.err.Error()
			}
		}
	case tea.KeyMsg:
		if m.input.Focused() {
			return m.updateInput(msg)
		}
		c := &m.cats[m.cat]
		n := len(c.items)
		switch msg.String() {
		case "left", "h", "shift+tab":
			m.cat = (m.cat - 1 + len(m.cats)) % len(m.cats)
		case "right", "l", "tab":
			m.cat = (m.cat + 1) % len(m.cats)
		case "up", "k":
			if n > 0 {
				c.pos = (c.pos - 1 + n) % n
			}
		case "down", "j":
			if n > 0 {
				c.pos = (c.pos + 1) % n
			}
		case "q", "esc":
			return tea.Quit
		case "s", "S":
			return m.scanAll()
		case "u", "U":
			if m.newVersion != "" {
				return Open(SelfUpdateID)
			}
		case "enter", " ":
			it, ok := m.current()
			switch {
			case !ok:
				return nil
			case it.set != nil:
				return m.activate(*it.set)
			}
			id := it.tool.ID
			// 工具页会重新扫描同一批目录，停掉首页的这次扫描，返回首页时再重扫
			if sum := m.sums[id]; sum != nil && !sum.done {
				sum.cancel()
			}
			return Open(id)
		}
	default:
		if m.input.Focused() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return cmd
		}
	}
	return nil
}

// activate 选项型设置切到下一个值，文字型设置打开输入框
func (m *Home) activate(s Setting) tea.Cmd {
	if len(s.Options) == 0 {
		m.editErr = ""
		m.input.SetValue(s.Get(m.env))
		m.input.CursorEnd()
		return m.input.Focus()
	}
	return m.apply(s, nextOption(s, m.env))
}

// nextOption 选项型设置的下一个值，当前值不在选项里时从第一个开始
func nextOption(s Setting, env Env) string {
	return s.Options[(slices.Index(s.Options, s.Get(env))+1)%len(s.Options)]
}

// updateInput 输入框里 enter 保存、esc 取消，其余按键交给输入框
func (m *Home) updateInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.editErr = ""
		return nil
	case "enter":
		it, _ := m.current()
		v := strings.TrimSpace(m.input.Value())
		c := m.env.Config
		if err := it.set.Set(&c, v); err != nil {
			m.editErr = err.Error()
			return nil
		}
		m.input.Blur()
		return m.apply(*it.set, v)
	}
	m.editErr = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// apply 把设置写入首页持有的配置，并通知根模型同步运行中的配置、写回配置文件
func (m *Home) apply(s Setting, v string) tea.Cmd {
	if err := s.Set(&m.env.Config, v); err != nil {
		m.editErr = err.Error()
		return nil
	}
	return func() tea.Msg {
		return ConfigMsg{Edit: func(c *config.Config) { _ = s.Set(c, v) }}
	}
}

// scanAll 并行扫描全部清理工具
func (m *Home) scanAll() tea.Cmd {
	for _, sum := range m.sums {
		if !sum.done {
			return nil
		}
	}
	m.gen++
	m.sums = map[string]*toolSum{}
	var cmds []tea.Cmd
	for _, t := range m.tools {
		if t.Source != nil {
			cmds = append(cmds, m.startScan(t))
		}
	}
	return tea.Batch(cmds...)
}

// Refresh 从工具页返回后重扫该工具；还没扫描过时什么也不做
func (m *Home) Refresh(id string) tea.Cmd {
	if m.sums[id] == nil {
		return nil
	}
	m.gen++
	for _, t := range m.tools {
		if t.ID == id && t.Source != nil {
			return m.startScan(t)
		}
	}
	return nil
}

// startScan 开始扫描一个工具，替换并取消该工具尚未完成的旧扫描
func (m *Home) startScan(t Tool) tea.Cmd {
	if old := m.sums[t.ID]; old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.sums[t.ID] = &toolSum{gen: m.gen, cancel: cancel}
	return scanTool(ctx, t, m.env, m.gen)
}

// total 已完成扫描的工具数和可释放总量；partial 表示总量可能偏小
func (m *Home) total() (done int, size int64, partial bool) {
	for _, s := range m.sums {
		if s.done {
			done++
			size += s.size
			partial = partial || s.partial
		}
	}
	return done, size, partial
}

func (m *Home) Crumbs() []string { return nil }

func (m *Home) Hints() []string {
	if m.input.Focused() {
		return []string{"enter", "保存", "esc", "取消"}
	}
	var hints []string
	if len(m.cats) > 1 {
		hints = append(hints, "←→", "分类")
	}
	if len(m.cats[m.cat].items) > 1 {
		hints = append(hints, "↑↓", "选择")
	}
	if it, ok := m.current(); ok {
		action := "打开"
		if it.set != nil {
			action = "切换"
			if len(it.set.Options) == 0 {
				action = "编辑"
			}
		}
		hints = append(hints, "enter", action)
	}
	scan := "扫描"
	if m.sums != nil {
		scan = "重新扫描"
	}
	hints = append(hints, "s", scan)
	if m.newVersion != "" {
		hints = append(hints, "U", "升级")
	}
	return append(hints, "t", "主题", "q", "退出")
}

func (m *Home) Status() string {
	tools := theme.MutedStyle.Render(strconv.Itoa(len(m.tools)) + " 个工具")
	if m.sums == nil {
		return tools
	}
	done, size, partial := m.total()
	if done < len(m.sums) {
		return theme.SubtleStyle.Render("正在扫描 "+strconv.Itoa(done)+"/"+strconv.Itoa(len(m.sums))) + theme.MutedStyle.Render(" · ") + tools
	}
	prefix := "共可释放 "
	if partial {
		prefix = "共可释放 ≥ "
	}
	return theme.Fg(theme.Accent).Render(prefix+fsx.FormatBytes(size)) + theme.MutedStyle.Render(" · ") + tools
}

func (m *Home) Body(w, h int) string {
	top := m.top(w-4, h)
	return widget.Inset(top + "\n" + m.bottom(w-4, h-lipgloss.Height(top)))
}

// brand 品牌区：很高时显示大字 Logo，较高时收成单行字标，再矮就省略
func (m *Home) brand(iw, h int) string {
	tagline := theme.SubtleStyle.Render("系统维护工具箱")
	switch {
	case h >= 46 && iw >= 60:
		return lipgloss.PlaceHorizontal(iw, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, theme.Logo(), "", tagline))
	case h >= 30:
		return lipgloss.PlaceHorizontal(iw, lipgloss.Center, theme.BrandGradient("◆ SYSBOX")+"   "+tagline)
	}
	return ""
}

// top 首页顶部：品牌区与分类方块，上下各留一行。
// 方块够高时带像素图标和汇总，再高一些上下留白，矮时只保留名称
func (m *Home) top(iw, h int) string {
	tiles := m.tiles(iw, h >= 22, h >= 38)
	if brand := m.brand(iw, h); brand != "" {
		return "\n" + brand + "\n\n" + tiles + "\n"
	}
	return "\n" + tiles + "\n"
}

// bottom 首页下半部分：列表、详情和系统概况
func (m *Home) bottom(iw, h int) string {
	mainH := max(4, h-2) // 底部系统信息及其上空行
	var main string
	switch {
	case iw >= 96:
		lw := iw * 53 / 100
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.list(lw, mainH), "  ", m.detail(iw-lw-2, mainH))
	case m.input.Focused():
		// 窄屏没有详情面板，输入框放在列表下方
		field := m.field(iw)
		main = m.list(iw, max(4, mainH-lipgloss.Height(field))) + "\n" + field
	default:
		main = m.list(iw, mainH)
	}
	return main + "\n\n" + m.infoLine(iw)
}

// stripBlock 打开工具页后顶部的分类条，上下各留一行
func (m *Home) stripBlock(iw int) string {
	var parts []string
	for i, c := range m.cats {
		if i == m.cat {
			parts = append(parts, lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface).Bold(true).
				Padding(0, 1).Render("◆ "+c.Name))
		} else {
			parts = append(parts, theme.MutedStyle.Padding(0, 1).Render("◇ "+c.Name))
		}
	}
	return "\n" + theme.Truncate(strings.Join(parts, "  "), iw) + "\n"
}

// Strip 工具页顶部的分类条，已包含页面左右边距
func (m *Home) Strip(w int) string { return widget.Inset(m.stripBlock(w - 4)) }

// Slide 进入或退出工具页的过渡画面。动画开始时渲染一次全部素材，逐帧只按进度截取，
// 保证每帧开销很小；下方内容按最终高度排版，动画途中不会重新排版
type Slide struct {
	W, H  int
	enter bool
	full  []string // 完整的首页顶部
	strip string   // 收起后的分类条
	below []string // 首页顶部下方的内容：进入时是工具页，退出时是首页下半部分
}

// NewSlide 准备一次过渡动画；enter 为 true 表示首页顶部向上滑出、工具页随之上移，退出时反向
func (m *Home) NewSlide(w, h int, enter bool, page func(w, h int) string) *Slide {
	iw := w - 4
	s := &Slide{W: w, H: h, enter: enter, strip: widget.Inset(m.stripBlock(iw))}
	s.full = strings.Split(widget.Inset(m.top(iw, h)), "\n")
	var below string
	if enter {
		below = page(w, h-lipgloss.Height(s.strip))
	} else {
		below = widget.Inset(m.bottom(iw, h-len(s.full)))
	}
	s.below = strings.Split(below, "\n")
	return s
}

// Duration 动画时长：按顶部要滑过的行数计算，保证以 frame 为帧间隔时，
// 缓动最快处每帧也只移动一行；时长限制在 180 到 340 毫秒之间
func (s *Slide) Duration(frame time.Duration) time.Duration {
	d := time.Duration(float64(s.span()) * math.Pi / 2 * float64(frame))
	return min(340*time.Millisecond, max(180*time.Millisecond, d))
}

// span 顶部需要滑过的行数
func (s *Slide) span() int { return len(s.full) - lipgloss.Height(s.strip) }

// Frame 进度 progress（0 到 1）对应的画面。顶部按缓动曲线逐行滑动，滑完换成分类条
func (s *Slide) Frame(progress float64) string {
	t := easeInOut(progress)
	if !s.enter {
		t = easeInOut(1 - progress)
	}
	span := s.span()
	top := s.strip
	if d := int(float64(span)*t + 0.5); d < span {
		top = strings.Join(s.full[d:], "\n")
	}
	rest := max(0, s.H-lipgloss.Height(top))
	lines := make([]string, rest)
	copy(lines, s.below)
	return top + "\n" + strings.Join(lines, "\n")
}

// easeInOut 正弦缓动：起止慢、中间快，最快处是平均速度的 π/2 倍
func easeInOut(t float64) float64 {
	t = min(1, max(0, t))
	return (1 - math.Cos(math.Pi*t)) / 2
}

// Focus 选中指定工具所在的分类和位置，打开工具页时分类条据此高亮
func (m *Home) Focus(id string) {
	for ci := range m.cats {
		for k, it := range m.cats[ci].items {
			if it.tool != nil && it.tool.ID == id {
				m.cat, m.cats[ci].pos = ci, k
				return
			}
		}
	}
}

// icons 是否在列表里显示像素图标
func (m *Home) icons() bool { return m.env.Config.Icons != "none" }

// tiles 顶部一排分类方块，等分整行宽度，余数补给最后一块；选中的方块用品牌渐变描边和上色
func (m *Home) tiles(w int, tall, pad bool) string {
	const gap = 2
	n := len(m.cats)
	base := (w - gap*(n-1)) / n
	var out []string
	for i, c := range m.cats {
		tw := base
		if i == n-1 {
			tw = w - (base+gap)*(n-1)
		}
		sel := i == m.cat
		name := theme.SubtleStyle.Bold(true).Render(c.Name)
		if sel {
			name = theme.Fg(theme.Accent).Bold(true).Render(c.Name)
		}
		var body []string
		if !tall {
			mark := theme.MutedStyle.Render("◇ ")
			if sel {
				mark = theme.BrandGradient("◆ ")
			}
			body = []string{mark + name}
		} else {
			text := []string{name, m.catCount(c), m.catSummary(c)}
			// 方块够宽时左侧放像素图标，文字按剩余宽度截断，否则只放文字
			if len(c.Icon) > 0 && tw-4 >= iconW(c.Icon)+3+14 {
				textW := tw - 4 - iconW(c.Icon) - 3
				text = []string{text[0], "", text[1], text[2]}
				for j := range text {
					text[j] = theme.Truncate(text[j], textW)
				}
				icon := theme.PixelArt(c.Icon, lipgloss.NewStyle(), !sel)
				text = strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, icon, "   ", strings.Join(text, "\n")), "\n")
			}
			body = text
			if pad {
				body = append(append([]string{""}, body...), "")
			}
		}
		out = append(out, tile(tw, body, sel))
		if i < n-1 {
			out = append(out, strings.Repeat(" ", gap))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, out...)
}

// iconW 像素图标的列数
func iconW(icon []string) int {
	w := 0
	for _, r := range icon {
		w = max(w, len([]rune(r)))
	}
	return w
}

// catCount 分类方块里的条目数
func (m *Home) catCount(c category) string {
	switch {
	case len(c.items) == 0:
		return theme.MutedStyle.Render("暂无工具")
	case c.items[0].set != nil:
		return theme.SubtleStyle.Render(strconv.Itoa(len(c.items)) + " 项设置")
	}
	return theme.SubtleStyle.Render(strconv.Itoa(len(c.items)) + " 个工具")
}

// catSummary 分类方块底部的一行：清理类显示扫描汇总，其他分类列出条目名
func (m *Home) catSummary(c category) string {
	var names []string
	scannable, done, pending, partial := false, 0, 0, false
	var size int64
	for _, it := range c.items {
		names = append(names, it.name())
		if it.tool == nil || it.tool.Source == nil {
			continue
		}
		scannable = true
		switch sum := m.sums[it.tool.ID]; {
		case sum == nil:
		case !sum.done:
			pending++
		default:
			done++
			size += sum.size
			partial = partial || sum.partial
		}
	}
	switch {
	case len(names) == 0:
		return theme.MutedStyle.Render("即将推出")
	case c.items[0].set != nil:
		return theme.MutedStyle.Render("修改后立即保存")
	case !scannable:
		return theme.MutedStyle.Render(strings.Join(names, "、"))
	case pending > 0:
		return theme.MutedStyle.Render("扫描中 " + strconv.Itoa(done) + "/" + strconv.Itoa(done+pending))
	case done == 0:
		return theme.MutedStyle.Render("按 s 扫描")
	case size == 0:
		return theme.MutedStyle.Render("无需清理")
	case partial:
		return theme.Fg(theme.Green).Bold(true).Render("可释放 ≥ " + fsx.FormatBytes(size))
	}
	return theme.Fg(theme.Green).Bold(true).Render("可释放 " + fsx.FormatBytes(size))
}

// tile 圆角方块：选中时边框从品牌紫渐变到青，否则用淡色细边框
func tile(w int, lines []string, sel bool) string {
	inner := w - 4 // 两侧边框与各 1 格内边距
	top, bottom := "╭"+strings.Repeat("─", w-2)+"╮", "╰"+strings.Repeat("─", w-2)+"╯"
	left, right := theme.FaintStyle.Render("│"), theme.FaintStyle.Render("│")
	if sel {
		top, bottom = theme.BrandGradient(top), theme.BrandGradient(bottom)
		left, right = theme.Fg(theme.Accent).Render("│"), theme.Fg(theme.Accent2).Render("│")
	} else {
		top, bottom = theme.FaintStyle.Render(top), theme.FaintStyle.Render(bottom)
	}
	out := []string{top}
	for _, l := range lines {
		out = append(out, left+" "+widget.Cell(l, inner, lipgloss.NewStyle(), false)+" "+right)
	}
	return strings.Join(append(out, bottom), "\n")
}

// box 圆角边框面板，内容区左右各留 1 格，按给定的外框尺寸填满
func box(w, h int, content string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Faint).
		Padding(0, 1).Width(max(1, w-2)).Height(max(1, h-2)).MaxHeight(h).Render(content)
}

// list 当前分类的条目列表：标题行、分割线，下面每项一行；空间够时条目之间加细分割线，不够时跟随光标滚动
func (m *Home) list(w, h int) string {
	c := m.cats[m.cat]
	iw, ih := w-4, h-2
	title := theme.Fg(theme.Accent2).Bold(true).Render(c.Name)
	hint := ""
	if len(c.items) > 1 {
		hint = theme.MutedStyle.Render("↑↓ 选择")
	}
	lines := []string{widget.Spread(iw, " "+title, hint+" "), theme.Rule(iw)}
	ih -= len(lines)
	if len(c.items) == 0 {
		lines = append(lines, "", "  "+theme.SubtleStyle.Render("即将推出"), "", "  "+theme.MutedStyle.Render("这个分类的功能还在规划中"))
		return box(w, h, strings.Join(lines, "\n"))
	}
	// 扫描过后按分类内最大值画出各工具可释放空间的比例条
	var peak int64
	for _, it := range c.items {
		if it.tool == nil {
			continue
		}
		if sum := m.sums[it.tool.ID]; sum != nil && sum.done {
			peak = max(peak, sum.size)
		}
	}
	var rows []string
	cur, span := 0, 1
	if m.icons() && iw >= 48 && ih >= 7 {
		// 卡片式：每项 2 行，左侧是像素图标，项与项之间空一行
		span = 2
		for k, it := range c.items {
			if k > 0 {
				rows = append(rows, "")
			}
			if k == c.pos {
				cur = len(rows)
			}
			rows = append(rows, m.card(it, k == c.pos, iw, peak)...)
		}
	} else {
		// 紧凑式：每项一行，空间够时项与项之间加细分割线
		spaced := 2*len(c.items)-1 <= ih
		for k, it := range c.items {
			if spaced && k > 0 {
				rows = append(rows, "  "+theme.FaintStyle.Render(strings.Repeat("┈", max(0, iw-4))))
			}
			if k == c.pos {
				cur = len(rows)
			}
			rows = append(rows, m.row(it, k == c.pos, iw, peak))
		}
	}
	off := min(cur, widget.Scroll(cur+span-1, 0, len(rows), ih))
	rows = rows[off:min(len(rows), off+ih)]
	return box(w, h, strings.Join(append(lines, rows...), "\n"))
}

// card 卡片式条目：左侧 2 行高的像素图标；右侧第一行是名称与数值，第二行是简介，
// 扫描后第二行右侧画出可释放空间的比例条。选中项整块铺底色并用左侧色条标出
func (m *Home) card(it item, sel bool, w int, peak int64) []string {
	bg := lipgloss.NewStyle()
	lead := bg.Render("  ")
	nameStyle := theme.TextStyle.Bold(true)
	if sel {
		bg = bg.Background(theme.Surface)
		lead = theme.Fg(theme.Accent).Inherit(bg).Render("▌ ")
		nameStyle = theme.Fg(theme.Accent).Bold(true)
	}
	const iw = 6 // 列表图标宽 6 列
	icon := []string{bg.Render(strings.Repeat(" ", iw)), bg.Render(strings.Repeat(" ", iw))}
	if len(it.icon()) > 0 {
		icon = strings.Split(theme.PixelArt(it.icon(), bg, false), "\n")
	}
	tw := w - 2 - iw - 3 - 1 // 色条、图标、间隔、右侧留白
	right, rightStyle, sum := m.value(it, sel)
	rightW := 0
	if right != "" {
		rightW = min(16, max(8, tw/3))
	}
	desc := cell(it.desc(), tw, theme.MutedStyle.Inherit(bg), false)
	if sum != nil && sum.done && peak > 0 && sum.size > 0 {
		desc = cell(it.desc(), tw-rightW, theme.MutedStyle.Inherit(bg), false) + meter(sum.size, peak, rightW, bg)
	}
	text := []string{
		cell(it.name(), tw-rightW, nameStyle.Inherit(bg), false) + cell(right, rightW, rightStyle.Inherit(bg), true),
		desc,
	}
	out := make([]string, 2)
	for i := range out {
		out[i] = lead + icon[i] + bg.Render("   ") + text[i] + bg.Render(" ")
	}
	return out
}

// value 条目右侧的数值：设置显示当前值，扫描过的清理工具显示可释放空间
func (m *Home) value(it item, sel bool) (string, lipgloss.Style, *toolSum) {
	if it.set != nil {
		style := theme.Fg(theme.Accent2)
		if sel {
			style = style.Bold(true)
		}
		return it.set.Show(it.set.Get(m.env)), style, nil
	}
	sum := m.sums[it.tool.ID]
	if sum == nil {
		return "", theme.MutedStyle, nil
	}
	text, style := sumText(sum)
	return strings.TrimPrefix(text, "可释放 "), style, sum
}

// row 紧凑式条目，一行显示名称、简介和数值；扫描后简介改为比例条
func (m *Home) row(it item, sel bool, w int, peak int64) string {
	bg := lipgloss.NewStyle()
	lead := "  "
	nameStyle := theme.TextStyle
	if sel {
		bg = bg.Background(theme.Surface)
		lead = theme.Fg(theme.Accent).Inherit(bg).Render("▌ ")
		nameStyle = theme.Fg(theme.Accent).Bold(true)
	}
	rest := w - 2 - 1
	nameW := min(20, max(10, rest*2/5))
	right, rightStyle, sum := m.value(it, sel)
	rightW := 0
	switch {
	case it.set != nil:
		rightW = min(24, max(8, rest/3))
	case right != "":
		rightW = min(14, max(8, rest/4))
	}
	midW := max(0, rest-nameW-rightW)
	mid := cell(it.desc(), midW, theme.MutedStyle.Inherit(bg), false)
	if bw := min(16, midW-2); sum != nil && sum.done && peak > 0 && sum.size > 0 && bw >= 4 {
		mid = meter(sum.size, peak, bw, bg) + bg.Render(strings.Repeat(" ", midW-bw))
	}
	return lead + cell(it.name(), nameW, nameStyle.Inherit(bg), false) + mid +
		cell(right, rightW, rightStyle.Inherit(bg), true) + bg.Render(" ")
}

// meter 可释放空间相对分类内最大值的比例条，占满给定宽度
func meter(size, peak int64, w int, bg lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	n := min(w, max(1, int(float64(size)/float64(peak)*float64(w)+0.5)))
	return theme.Fg(theme.Green).Inherit(bg).Render(strings.Repeat("━", n)) +
		theme.FaintStyle.Inherit(bg).Render(strings.Repeat("━", w-n))
}

// cell 定宽单元格，留白也带上样式的底色，选中行的底色才能连成一片
func cell(text string, w int, style lipgloss.Style, right bool) string {
	if w <= 0 {
		return ""
	}
	align := lipgloss.Left
	if right {
		align = lipgloss.Right
	}
	return style.Width(w).Align(align).Render(theme.Truncate(text, w))
}

// sumText 扫描汇总的简短文字与样式
func sumText(sum *toolSum) (string, lipgloss.Style) {
	switch {
	case !sum.done:
		return "扫描中…", theme.MutedStyle
	case sum.failure != "":
		return "不可用", theme.MutedStyle
	case sum.n == 0:
		return "无需清理", theme.MutedStyle
	case sum.partial:
		return "可释放 ≥ " + fsx.FormatBytes(sum.size), theme.Fg(theme.Green)
	}
	return "可释放 " + fsx.FormatBytes(sum.size), theme.Fg(theme.Green)
}

// detail 右侧详情面板：标题、简介、分割线，下面是扫描结果或当前值、说明要点和操作提示
func (m *Home) detail(w, h int) string {
	inner := widget.PanelInner(w)
	it, ok := m.current()
	if !ok {
		body := theme.Fg(theme.Accent).Bold(true).Render(m.cats[m.cat].Name) + "\n" +
			theme.MutedStyle.Render("即将推出，敬请期待")
		return widget.Panel(w, h, theme.Faint, body)
	}
	head := theme.Fg(theme.Accent).Bold(true).Render(it.name()) + "\n" + theme.MutedStyle.Width(inner).Render(it.desc())
	if m.icons() && len(it.icon()) > 0 {
		// 标题左侧放同一个像素图标，文字垂直居中在图标旁
		textW := inner - iconW(it.icon()) - 3
		head = lipgloss.JoinHorizontal(lipgloss.Center, theme.PixelArt(it.icon(), lipgloss.NewStyle(), false), "   ",
			theme.Fg(theme.Accent).Bold(true).Render(theme.Truncate(it.name(), textW))+"\n"+
				theme.MutedStyle.Render(theme.Truncate(it.desc(), textW)))
	}
	lines := []string{head, "", theme.Rule(inner), ""}
	var detail []string
	var action string
	if t := it.tool; t != nil {
		detail = t.Detail
		action = theme.Key("enter") + " " + theme.Fg(theme.Accent).Render("打开")
		switch sum := m.sums[t.ID]; {
		case sum != nil && sum.failure != "":
			lines = append(lines, theme.Fg(theme.Amber).Width(inner).Render(sum.failure), "")
		case sum != nil:
			text, style := sumText(sum)
			extra := ""
			if sum.done && sum.n > 0 {
				extra = " · 默认勾选 " + strconv.Itoa(sum.n) + " 项"
				if sum.partial {
					extra += " · 部分目录无法读取"
				}
			}
			lines = append(lines, style.Bold(true).Render(text)+theme.MutedStyle.Render(extra), "")
		case t.Source != nil:
			lines = append(lines, theme.MutedStyle.Render("按 s 扫描可释放空间"), "")
		}
	} else {
		s := it.set
		detail = s.Detail
		value := s.Show(s.Get(m.env))
		lines = append(lines, widget.KV("当前", 6, theme.Fg(theme.Accent2).Bold(true).Render(value)), "")
		switch {
		case m.input.Focused():
			lines = append(lines, m.field(inner), "")
			action = theme.Key("enter") + " " + theme.Fg(theme.Accent).Render("保存") + "   " +
				theme.Key("esc") + " " + theme.MutedStyle.Render("取消")
		case len(s.Options) > 0:
			action = theme.Key("enter") + " " + theme.Fg(theme.Accent).Render("切换为 "+s.Show(nextOption(*s, m.env)))
		default:
			action = theme.Key("enter") + " " + theme.Fg(theme.Accent).Render("编辑")
		}
	}
	for _, d := range detail {
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			theme.Fg(theme.Accent).Render("· "), theme.TextStyle.Width(inner-2).Render(d)))
	}
	lines = append(lines, "", action)
	return widget.Panel(w, h, theme.Faint, strings.Join(lines, "\n"))
}

// field 设置的输入框，输入不合法时下方显示原因
func (m *Home) field(w int) string {
	m.input.Width = max(4, w-5)
	out := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Accent).
		Padding(0, 1).Width(w - 2).Render(m.input.View())
	if m.editErr != "" {
		out += "\n" + theme.Fg(theme.Amber).Width(w).Render(m.editErr)
	}
	return out
}

// infoLine 系统概况：左侧系统版本，右侧数据盘占用条，占用条拉伸填满中间
func (m *Home) infoLine(w int) string {
	i := m.info
	if i.total == 0 {
		return theme.MutedStyle.Render("正在读取系统信息…")
	}
	label := theme.MutedStyle.Render("数据盘 ")
	free := " " + theme.SubtleStyle.Render("可用 "+fsx.FormatBytes(i.free)+" / "+fsx.FormatBytes(i.total))
	left := theme.SubtleStyle.Render(i.os) + "    "
	barW := w - lipgloss.Width(left) - lipgloss.Width(label) - lipgloss.Width(free)
	if barW < 12 {
		left = ""
		barW = w - lipgloss.Width(label) - lipgloss.Width(free)
	}
	barW = max(4, barW)
	used := int(float64(i.total-i.free) / float64(i.total) * float64(barW))
	bar := theme.BrandGradient(strings.Repeat("━", used)) + theme.FaintStyle.Render(strings.Repeat("━", barW-used))
	return left + label + bar + free
}
