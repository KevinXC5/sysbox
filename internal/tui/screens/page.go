// Package screens 包含 sysbox 的全部页面。
// 页面只负责主体内容、面包屑、按键提示和状态；顶栏、底栏和全局按键由根模型统一处理。
package screens

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// Env 页面共享的运行环境
type Env struct {
	DryRun bool // 演练模式：完整走流程，但不做任何修改
	Config config.Config
}

// Page 一个页面
type Page interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	// Crumbs 面包屑，首页为空
	Crumbs() []string
	// Hints 底栏按键提示，按“按键, 说明”成对排列
	Hints() []string
	// Status 底栏右侧状态
	Status() string
	// Body 主体区域
	Body(w, h int) string
	// Busy 执行中不允许返回或切换主题
	Busy() bool
}

// Tool 首页上的工具入口
type Tool struct {
	ID     string
	Group  string
	Name   string
	Desc   string   // 列表里的一句话简介
	Detail []string // 详情面板里的功能要点
	New    func(Env) Page
	// Source 清理类工具的数据源，首页用它汇总可释放空间；非清理工具为 nil
	Source func(Env) (cleanup.Source, error)
}

// BackMsg 返回首页
type BackMsg struct{}

// OpenMsg 打开指定工具
type OpenMsg struct{ ID string }

// Back 返回首页的命令
func Back() tea.Msg { return BackMsg{} }

// Open 打开工具的命令
func Open(id string) tea.Cmd { return func() tea.Msg { return OpenMsg{id} } }

// dryRunNote 演练模式下各页面统一使用的提示
func dryRunNote() string {
	return theme.Fg(theme.Amber).Render("◌ 演练模式：只展示将要执行的操作，不会做任何修改")
}

// errorBox 页面级错误提示
func errorBox(w int, title, msg string) string {
	dw := min(64, w-4)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Faint).
		Padding(1, 3).Width(dw - 2).Render(
		theme.Fg(theme.Amber).Bold(true).Render("◌ "+title) + "\n\n" +
			theme.SubtleStyle.Width(dw-8).Render(msg))
}

// Typer 可选接口：页面处于文字输入状态时返回 true，根模型不再拦截单字母快捷键
type Typer interface {
	Typing() bool
}

// errorPage 工具初始化失败时显示的页面
type errorPage struct {
	crumbs []string
	err    error
}

// NewErrorPage 创建错误页
func NewErrorPage(crumbs []string, err error) Page { return &errorPage{crumbs, err} }

func (p *errorPage) Init() tea.Cmd    { return nil }
func (p *errorPage) Busy() bool       { return false }
func (p *errorPage) Crumbs() []string { return p.crumbs }
func (p *errorPage) Hints() []string  { return []string{"enter", "返回首页"} }
func (p *errorPage) Status() string   { return "" }
func (p *errorPage) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "enter" || k.String() == "esc" || k.String() == "q") {
		return Back
	}
	return nil
}
func (p *errorPage) Body(w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, errorBox(w, "无法打开", p.err.Error()))
}
