package screens

import (
	"cmp"
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KevinXC5/sysbox/internal/tools/disk"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// diskLevel 保留各层的选中项、查询和滚动位置；屏幕仅展示最近三层。
type diskLevel struct {
	target, filter string
	result         systemResult
	visible        []systemRow
	cursor, offset int
}
type diskPage struct {
	*systemPage
	state               *diskState
	levels              []diskLevel
	sortName, ascending bool
}

func (p *diskPage) snapshot() diskLevel {
	return diskLevel{p.target, p.filter, p.result, append([]systemRow(nil), p.visible...), p.cursor, p.offset}
}
func (p *diskPage) order() {
	row, had := p.current()
	sort.SliceStable(p.visible, func(i, j int) bool {
		a, b := p.visible[i], p.visible[j]
		if a.key == "rest" || b.key == "rest" {
			return b.key == "rest" && a.key != "rest"
		}
		comparison := cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
		if !p.sortName {
			an, aok := a.value.(*disk.Node)
			bn, bok := b.value.(*disk.Node)
			if aok && bok {
				if an.Bytes != bn.Bytes {
					comparison = cmp.Compare(an.Bytes, bn.Bytes)
				} else {
					return comparison < 0
				}
			} else {
				return comparison < 0
			}
		}
		if p.ascending {
			return comparison < 0
		}
		return comparison > 0
	})
	if had {
		for i, r := range p.visible {
			if r.id() == row.id() {
				p.cursor = i
				break
			}
		}
	}
}
func (p *diskPage) Hints() []string {
	if p.Typing() {
		return p.systemPage.Hints()
	}
	return []string{"tab", "类别", "←→", "返回 / 进入", "↑↓", "选择", "n", "名称排序", "s", "大小排序", "/", "搜索", "r", "重扫", "esc", "返回"}
}
func (p *diskPage) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok && !p.Typing() && !p.acting {
		switch k.String() {
		case "n", "s":
			byName := k.String() == "n"
			if p.sortName == byName {
				p.ascending = !p.ascending
			} else {
				p.sortName = byName
				p.ascending = byName
			}
			p.order()
			return nil
		case "left", "backspace":
			if p.loading {
				return nil
			}
			if p.tab == 0 && len(p.levels) > 0 {
				level := p.levels[len(p.levels)-1]
				p.levels = p.levels[:len(p.levels)-1]
				p.target, p.filter, p.result, p.visible, p.cursor, p.offset = level.target, level.filter, level.result, level.visible, level.cursor, level.offset
				p.focus = 0
				p.order()
				return nil
			}
			if p.tab == 0 {
				return p.systemPage.key(tea.KeyMsg{Type: tea.KeyBackspace})
			}
			return nil
		case "right", "enter":
			if p.loading {
				return nil
			}
			if row, ok := p.current(); ok && row.target != "" {
				if p.tab == 0 {
					p.levels = append(p.levels, p.snapshot())
				} else {
					p.levels = nil
				}
				return p.navigate(row.target, row.targetTab)
			}
			return nil
		case "r":
			// 重扫以当前目录为新的扫描起点，避免保留上层的旧统计。
			p.levels = nil
			p.cache = map[string]systemResult{}
		case "tab", "shift+tab":
			p.levels = nil
		}
	}
	reset := false
	if k, ok := msg.(tea.KeyMsg); ok && p.inputMode == "target" && k.String() == "enter" && strings.TrimSpace(p.input.Value()) != "" {
		reset = true
	}
	cmd := p.systemPage.Update(msg)
	p.focus = 0
	if reset {
		p.levels = nil
	}
	if m, ok := msg.(systemActedMsg); ok && m.err == nil && m.change {
		// 移到废纸篓后，从已更新的目录树重建所有可返回的层级。
		for i := range p.levels {
			level := &p.levels[i]
			result, err := p.state.loadTree(context.Background(), 0, level.target, false)
			if err == nil {
				q := newSystemPage(p.env, p.spec)
				q.result = result
				q.filter = level.filter
				q.visible = level.visible
				q.cursor = level.cursor
				q.rebuild(true)
				level.result, level.visible, level.cursor = result, q.visible, q.cursor
			}
		}
	}
	p.order()
	return cmd
}
func (p *diskPage) panes(_ *systemPage, w, h int) string {
	if p.tab != 0 {
		return p.listPane(w, h)
	}
	// 窄终端减少可见栏数，始终让当前目录留在屏幕内。
	count := min(3, max(1, (w+2)/30))
	width := (w - 2*(count-1)) / count
	start := max(0, len(p.levels)-(count-1))
	var panes []string
	for _, level := range p.levels[start:] {
		q := newSystemPage(p.env, p.spec)
		q.target, q.result, q.visible, q.cursor, q.offset, q.filter = level.target, level.result, level.visible, level.cursor, level.offset, level.filter
		q.focus = 1
		q.spec.tabs = []string{diskColumnTitle(level.target)}
		q.tab = 0
		panes = append(panes, q.listPane(width, h))
	}
	tabs := p.spec.tabs
	p.spec.tabs = append([]string(nil), tabs...)
	label := "大小 ↓"
	if p.sortName {
		label = "名称 ↓"
	}
	if p.ascending {
		label = strings.ReplaceAll(label, "↓", "↑")
	}
	p.spec.tabs[0] = diskColumnTitle(p.target) + " · " + label
	panes = append(panes, p.listPane(width, h))
	p.spec.tabs = tabs
	for len(panes) < count {
		panes = append(panes, widget.Panel(width, h, theme.Faint, theme.Truncate(theme.MutedStyle.Render("→ 进入目录后显示下一层"), max(1, widget.PanelInner(width)))))
	}
	// 最后一栏吸收除法余数，保持整体宽度稳定。
	if rest := w - (width*count + 2*(count-1)); rest > 0 {
		panes[len(panes)-1] = lipgloss.NewStyle().Width(width + rest).Render(panes[len(panes)-1])
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, interleaveDiskPanes(panes)...)
}
func interleaveDiskPanes(panes []string) []string {
	var result []string
	for i, pane := range panes {
		if i > 0 {
			result = append(result, "  ")
		}
		result = append(result, pane)
	}
	return result
}

func diskColumnTitle(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		return path
	}
	return name
}
