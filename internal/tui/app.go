// Package tui 是 sysbox 的界面根模型：负责页面路由、顶栏底栏和全局按键。
package tui

import (
	"context"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/selfupdate"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/ui/widget"
)

// 界面最小尺寸，小于它时只提示放大窗口
const minW, minH = 60, 16

// newVersionMsg 后台检查到的新版本
type newVersionMsg struct{ version string }

// systemAppearanceMsg 将后台读取的系统外观交给界面线程处理。
type systemAppearanceMsg struct{ dark bool }

// watchSystemAppearance 持续检测系统外观，让静止的页面也能自动更新配色。
func watchSystemAppearance() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return systemAppearanceMsg{dark: theme.SystemDark()}
	})
}

// App 根模型
type App struct {
	w, h       int
	env        screens.Env
	home       *screens.Home
	page       screens.Page // 非空时显示该页面，否则显示首页
	pageID     string       // 当前页面对应的工具
	start      string       // 启动后直接打开的工具
	newVersion string
	restart    bool
}

// New 创建应用；start 非空时启动后直接打开该工具
func New(env screens.Env, start string) *App {
	return &App{env: env, home: screens.NewHome(Tools(), env), start: start}
}

// Restart 退出后是否需要用新版本重启
func (a *App) Restart() bool { return a.restart }

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.home.Init(), a.checkUpdate(), watchSystemAppearance()}
	if a.start != "" {
		cmds = append(cmds, screens.Open(a.start))
	}
	return tea.Batch(cmds...)
}

// checkUpdate 启动时在后台检查一次新版本；本地构建或在配置中关闭时跳过，出错静默忽略
func (a *App) checkUpdate() tea.Cmd {
	if meta.IsDev() || a.env.Config.Update.DisableCheck {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rel, err := selfupdate.NewClient(meta.Repo).Latest(ctx)
		if err != nil || !selfupdate.Newer(rel.Tag, meta.Version) {
			return nil
		}
		return newVersionMsg{rel.Tag}
	}
}

// active 当前显示的页面
func (a *App) active() screens.Page {
	if a.page != nil {
		return a.page
	}
	return a.home
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case systemAppearanceMsg:
		theme.UpdateSystemAppearance(msg.dark)
		return a, watchSystemAppearance()
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		return a, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if msg.String() == "t" && !a.active().Busy() && !typing(a.active()) {
			return a, a.cycleTheme()
		}
	case newVersionMsg:
		a.newVersion = msg.version
		a.home.SetNewVersion(msg.version)
		return a, nil
	case screens.OpenMsg:
		return a, a.open(msg.ID)
	case screens.BackMsg:
		// 清理后回到首页，已扫描过的汇总随之更新
		a.page = nil
		return a, a.home.Refresh(a.pageID)
	case screens.RestartMsg:
		a.restart = true
		return a, tea.Quit
	}
	return a, a.active().Update(msg)
}

func typing(p screens.Page) bool {
	t, ok := p.(screens.Typer)
	return ok && t.Typing()
}

// open 打开工具或自升级页
func (a *App) open(id string) tea.Cmd {
	if id == screens.SelfUpdateID {
		a.page, a.pageID = screens.NewSelfUpdate(a.env), id
		return a.page.Init()
	}
	t, ok := Find(id)
	if !ok {
		return nil
	}
	a.page, a.pageID = t.New(a.env), id
	return a.page.Init()
}

// cycleTheme 同步切换主题，保证本次按键后的重绘就用上新主题；写配置放到后台
func (a *App) cycleTheme() tea.Cmd {
	next := theme.NextMode()
	theme.SetMode(next)
	return func() tea.Msg {
		c, _ := config.Load()
		c.Theme = next.String()
		_ = config.Save(c)
		return nil
	}
}

func (a *App) View() string {
	if a.w == 0 {
		return ""
	}
	if a.w < minW || a.h < minH {
		tip := theme.SubtleStyle.Render("窗口太小，请放大到至少 "+strconv.Itoa(minW)+" × "+strconv.Itoa(minH)) + "\n" +
			theme.MutedStyle.Render("当前 "+strconv.Itoa(a.w)+" × "+strconv.Itoa(a.h))
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, tip)
	}
	p := a.active()
	head := widget.Header(a.w, p.Crumbs(), widget.HeaderInfo{DryRun: a.env.DryRun, NewVersion: a.newVersion})
	foot := widget.Footer(a.w, p.Hints(), p.Status())
	bodyH := max(1, a.h-lipgloss.Height(head)-lipgloss.Height(foot))
	return widget.Frame(a.w, a.h, head, p.Body(a.w, bodyH), foot)
}
