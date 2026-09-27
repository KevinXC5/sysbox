package screens

import (
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/fsx"
)

// 通用清理页：扫描 → 分类浏览与勾选 → 检查 → 确认 → 逐项删除 → 结果。
// 具体扫描什么、如何删除由 cleanup.Source 决定。

type cleanState int

const (
	cleanScanning cleanState = iota
	cleanList
	cleanChecking
	cleanBlocked
	cleanConfirm
	cleanDeleting
	cleanDone
	cleanError
)

// 扫描页至少展示的时长，避免一闪而过
const minScanTime = 700 * time.Millisecond

// 页面内部消息
type (
	cleanStageMsg    struct{ stage string }
	cleanScannedMsg  struct{ items []cleanup.Item }
	cleanMeasuredMsg struct {
		idx  int
		size int64
	}
	cleanRootsMsg   struct{ sizes []int64 }
	cleanFailedMsg  struct{ err error }
	cleanScanDone   struct{}
	cleanScanReady  struct{}
	cleanCheckedMsg struct{ notice *cleanup.Notice }
	cleanRemovedMsg struct {
		idx int
		err error
	}
	cleanAfterMsg struct{ sizes []int64 }
)

// cleanLog 删除过程中的一条记录
type cleanLog struct {
	item cleanup.Item
	err  error
}

type cleanPage struct {
	src    cleanup.Source
	cats   []cleanup.Category
	dryRun bool
	state  cleanState
	err    error

	// 扫描
	ch        chan tea.Msg
	stage     string
	scanStart time.Time
	items     []cleanup.Item
	measured  map[int]bool
	pending   int // 待统计大小的条目数
	selected  map[int]bool
	before    []int64 // 各根目录清理前大小
	after     []int64

	// 浏览
	tabs   []int // 可见分类的下标
	tab    int   // 当前标签在 tabs 中的位置
	cursor map[int]int
	offset map[int]int

	spin    spinner.Model
	focusOK bool
	notice  *cleanup.Notice

	// 删除
	queue   []int
	pos     int
	target  int64
	freed   int64
	failed  int
	logs    []cleanLog
	start   time.Time
	elapsed time.Duration
}

// NewClean 基于数据源创建清理页
func NewClean(src cleanup.Source, env Env) Page {
	return &cleanPage{
		src:    src,
		cats:   src.Categories(),
		dryRun: env.DryRun,
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *cleanPage) Init() tea.Cmd {
	m.scanStart = time.Now()
	m.ch = make(chan tea.Msg, 64)
	go m.scan(m.ch)
	return tea.Batch(m.spin.Tick, m.wait())
}

// scan 在后台扫描、统计大小，通过通道把进度逐条发回界面
func (m *cleanPage) scan(ch chan<- tea.Msg) {
	defer close(ch)
	items, err := m.src.Scan(func(stage string) { ch <- cleanStageMsg{stage} })
	if err != nil {
		ch <- cleanFailedMsg{err}
		return
	}
	ch <- cleanScannedMsg{items}

	var todo []int
	for i, it := range items {
		if !it.Sized {
			todo = append(todo, i)
		}
	}
	if len(todo) > 0 {
		ch <- cleanStageMsg{"统计占用空间"}
		paths := make([]string, len(todo))
		for i, idx := range todo {
			paths[i] = items[idx].Path
		}
		fsx.MeasureAll(paths, 8, func(i int, size int64) { ch <- cleanMeasuredMsg{todo[i], size} })
	}
	ch <- cleanRootsMsg{m.rootSizes()}
}

func (m *cleanPage) rootSizes() []int64 {
	roots := m.src.Roots()
	sizes := make([]int64, len(roots))
	for i, r := range roots {
		sizes[i] = fsx.DiskUsage(r.Path)
	}
	return sizes
}

// wait 读取扫描通道的下一条消息，通道关闭即扫描结束
func (m *cleanPage) wait() tea.Cmd {
	ch := m.ch
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return cleanScanDone{}
		}
		return msg
	}
}

func (m *cleanPage) Busy() bool { return m.state == cleanDeleting }

func (m *cleanPage) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd

	case cleanStageMsg:
		m.stage = msg.stage
		return m.wait()
	case cleanScannedMsg:
		m.items = msg.items
		m.measured = map[int]bool{}
		for i, it := range m.items {
			if it.Sized {
				m.measured[i] = true
			} else {
				m.pending++
			}
		}
		return m.wait()
	case cleanMeasuredMsg:
		m.items[msg.idx].Size = msg.size
		m.measured[msg.idx] = true
		return m.wait()
	case cleanRootsMsg:
		m.before = msg.sizes
		return m.wait()
	case cleanFailedMsg:
		m.state, m.err = cleanError, msg.err
		return nil
	case cleanScanDone:
		if m.state == cleanError {
			return nil
		}
		if d := minScanTime - time.Since(m.scanStart); d > 0 {
			return tea.Tick(d, func(time.Time) tea.Msg { return cleanScanReady{} })
		}
		m.finishScan()
	case cleanScanReady:
		m.finishScan()

	case cleanCheckedMsg:
		m.notice = msg.notice
		if m.notice != nil && m.notice.Blocking {
			m.state = cleanBlocked
		} else {
			m.state, m.focusOK = cleanConfirm, false
		}
	case cleanRemovedMsg:
		return m.onRemoved(msg)
	case cleanAfterMsg:
		m.after = msg.sizes
		m.state = cleanDone

	case tea.KeyMsg:
		return m.onKey(msg.String())
	}
	return nil
}

// finishScan 按分类、大小排序，确定可见标签和默认勾选
func (m *cleanPage) finishScan() {
	sort.SliceStable(m.items, func(i, j int) bool {
		a, b := m.items[i], m.items[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Size > b.Size
	})
	m.selected = map[int]bool{}
	counts := make([]int, len(m.cats))
	for i, it := range m.items {
		m.selected[i] = it.Selectable && it.Selected
		counts[it.Category]++
	}
	// 主分类总是显示，其余分类没有条目时隐藏
	m.tabs = nil
	for i, c := range m.cats {
		if c.Primary || counts[i] > 0 {
			m.tabs = append(m.tabs, i)
		}
	}
	m.cursor, m.offset = map[int]int{}, map[int]int{}
	m.tab = 0
	for i, cat := range m.tabs {
		if counts[cat] > 0 {
			m.tab = i
			break
		}
	}
	m.state = cleanList
}

// ---------- 按键 ----------

func (m *cleanPage) onKey(k string) tea.Cmd {
	switch m.state {
	case cleanList:
		return m.onListKey(k)
	case cleanConfirm:
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.focusOK = !m.focusOK
		case "y":
			return m.startDelete()
		case "enter":
			if m.focusOK {
				return m.startDelete()
			}
			m.state = cleanList
		case "esc", "n", "q":
			m.state = cleanList
		}
	case cleanBlocked:
		switch k {
		case "r":
			return m.check()
		case "enter", "esc", "q":
			m.state = cleanList
		}
	case cleanDone:
		switch k {
		case "r":
			*m = *NewClean(m.src, Env{DryRun: m.dryRun}).(*cleanPage)
			return m.Init()
		case "enter", "esc", "q":
			return Back
		}
	case cleanError, cleanScanning:
		if k == "enter" || k == "esc" || k == "q" {
			return Back
		}
	}
	return nil
}

func (m *cleanPage) onListKey(k string) tea.Cmd {
	cat := m.curCat()
	idxs := m.tabItems()
	c := m.cursor[cat]
	switch k {
	case "up", "k":
		m.cursor[cat] = max(0, c-1)
	case "down", "j":
		m.cursor[cat] = min(max(0, len(idxs)-1), c+1)
	case "left", "h", "shift+tab":
		m.tab = (m.tab - 1 + len(m.tabs)) % len(m.tabs)
	case "right", "l", "tab":
		m.tab = (m.tab + 1) % len(m.tabs)
	case " ":
		if len(idxs) > 0 && m.items[idxs[c]].Selectable {
			m.selected[idxs[c]] = !m.selected[idxs[c]]
		}
	case "a":
		m.toggleAll(idxs)
	case "enter":
		if m.selCount() > 0 {
			return m.check()
		}
	case "esc", "q":
		return Back
	}
	return nil
}

// toggleAll 当前分类里可勾选的条目全选或全不选
func (m *cleanPage) toggleAll(idxs []int) {
	all := true
	for _, i := range idxs {
		if m.items[i].Selectable && !m.selected[i] {
			all = false
		}
	}
	for _, i := range idxs {
		if m.items[i].Selectable {
			m.selected[i] = !all
		}
	}
}

// check 删除前检查，在后台执行
func (m *cleanPage) check() tea.Cmd {
	m.state = cleanChecking
	src := m.src
	return tea.Batch(m.spin.Tick, func() tea.Msg { return cleanCheckedMsg{src.Check()} })
}

// ---------- 删除 ----------

func (m *cleanPage) startDelete() tea.Cmd {
	m.queue, m.target = nil, 0
	for i, it := range m.items {
		if m.selected[i] {
			m.queue = append(m.queue, i)
			m.target += it.Size
		}
	}
	m.pos, m.freed, m.failed, m.logs = 0, 0, 0, nil
	m.start = time.Now()
	m.state = cleanDeleting
	return tea.Batch(m.spin.Tick, m.removeCmd(m.queue[0]))
}

// removeCmd 删除单个条目；每项至少停留一小段时间，让进度变化可感知
func (m *cleanPage) removeCmd(idx int) tea.Cmd {
	it, src, dry := m.items[idx], m.src, m.dryRun
	step := 40 * time.Millisecond
	if dry {
		step = 140 * time.Millisecond
	}
	return func() tea.Msg {
		t0 := time.Now()
		var err error
		if !dry {
			err = src.Remove(it)
		}
		if d := step - time.Since(t0); d > 0 {
			time.Sleep(d)
		}
		return cleanRemovedMsg{idx, err}
	}
}

func (m *cleanPage) onRemoved(msg cleanRemovedMsg) tea.Cmd {
	it := m.items[msg.idx]
	m.logs = append(m.logs, cleanLog{it, msg.err})
	if msg.err != nil {
		m.failed++
	} else {
		m.freed += it.Size
	}
	m.pos++
	if m.pos < len(m.queue) {
		return m.removeCmd(m.queue[m.pos])
	}

	m.elapsed = time.Since(m.start)
	if m.dryRun {
		m.after = m.estimateAfter()
		m.state = cleanDone
		return nil
	}
	return func() tea.Msg { return cleanAfterMsg{m.rootSizes()} }
}

// estimateAfter 演练模式按释放量推算各根目录清理后的大小
func (m *cleanPage) estimateAfter() []int64 {
	roots := m.src.Roots()
	after := append([]int64(nil), m.before...)
	for _, l := range m.logs {
		if l.err != nil {
			continue
		}
		for i, r := range roots {
			if i < len(after) && within(l.item.Path, r.Path) {
				after[i] -= l.item.Size
			}
		}
	}
	return after
}

// ---------- 数据辅助 ----------

// curCat 当前标签对应的分类下标
func (m *cleanPage) curCat() int {
	if len(m.tabs) == 0 {
		return 0
	}
	return m.tabs[m.tab]
}

// tabItems 当前分类下的条目下标
func (m *cleanPage) tabItems() []int {
	return m.catItems(m.curCat())
}

func (m *cleanPage) catItems(cat int) []int {
	var out []int
	for i, it := range m.items {
		if it.Category == cat {
			out = append(out, i)
		}
	}
	return out
}

func (m *cleanPage) selCount() int {
	n := 0
	for _, v := range m.selected {
		if v {
			n++
		}
	}
	return n
}

func (m *cleanPage) selBytes() int64 {
	var b int64
	for i, v := range m.selected {
		if v {
			b += m.items[i].Size
		}
	}
	return b
}

// catStats 某分类的条目数、总大小，以及已勾选的数量和大小
func (m *cleanPage) catStats(cat int) (n int, size int64, selN int, selSize int64) {
	for i, it := range m.items {
		if it.Category != cat {
			continue
		}
		n++
		size += it.Size
		if m.selected[i] {
			selN++
			selSize += it.Size
		}
	}
	return
}

// selectedIrreversible 是否勾选了删除后无法恢复的条目
func (m *cleanPage) selectedIrreversible() bool {
	for i, v := range m.selected {
		if v && m.items[i].Irreversible {
			return true
		}
	}
	return false
}

func within(p, root string) bool {
	return len(p) > len(root) && p[:len(root)] == root && p[len(root)] == '/'
}
