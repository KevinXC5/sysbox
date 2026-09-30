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

// 进入和退出工具页的过渡动画按真实时间推进，每 animStep 刷新一帧（约 120 帧每秒）；
// 时长由过渡画面按滑动行数决定，生成画面之前先按 animDuration 计
const (
	animDuration = 260 * time.Millisecond
	animStep     = 8 * time.Millisecond
)

// FPS 界面刷新帧率上限，Bubble Tea 支持的最大值
const FPS = 120

// transition 正在播放的过渡动画；seq 区分前后两次动画，丢弃过期的帧消息
type transition struct {
	enter bool // true 为首页收起、工具页展开，false 为返回首页
	start time.Time
	dur   time.Duration
	seq   int
	slide *screens.Slide // 首次渲染时生成，窗口尺寸变化时重建
}

// progress 已播放的比例
func (t *transition) progress() float64 {
	return min(1, float64(time.Since(t.start))/float64(t.dur))
}

type animMsg struct{ seq int }

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

type pageMsg struct {
	page screens.Page
	msg  tea.Msg
}

func pageCommand(p screens.Page, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmds := make([]tea.Cmd, len(batch))
			for i, c := range batch {
				cmds[i] = pageCommand(p, c)
			}
			return tea.BatchMsg(cmds)
		}
		if msg == nil {
			return nil
		}
		return pageMsg{p, msg}
	}
}

func closePage(p screens.Page) {
	if c, ok := p.(interface{ Close() }); ok {
		c.Close()
	}
}

// configSavedMsg 配置写回完成
type configSavedMsg struct{ err error }

// App 根模型
type App struct {
	saving      bool                   // 正在写配置文件
	edits       []func(*config.Config) // 等待写回的修改，按顺序应用
	configError error
	watching    bool
	w, h        int
	env         screens.Env
	home        *screens.Home
	page        screens.Page // 非空时显示该页面，否则显示首页
	pageID      string       // 当前页面对应的工具
	start       string       // 启动后直接打开的工具
	newVersion  string
	restart     bool
	anim        *transition // 正在播放的过渡动画
	animSeq     int
}

// New 创建应用；start 非空时启动后直接打开该工具
func New(env screens.Env, start string) *App {
	return &App{env: env, home: screens.NewHome(Groups(), Tools(), Settings(), env), start: start}
}

// Restart 退出后是否需要用新版本重启
func (a *App) Restart() bool { return a.restart }

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{pageCommand(a.home, a.home.Init()), a.checkUpdate(), a.watchAppearance()}
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
	if owned, ok := msg.(pageMsg); ok {
		if owned.page == a.home {
			switch owned.msg.(type) {
			case screens.OpenMsg, screens.ConfigMsg, tea.QuitMsg:
				msg = owned.msg
			default:
				return a, pageCommand(a.home, a.home.Update(owned.msg))
			}
		} else {
			if owned.page != a.page {
				return a, nil
			}
			msg = owned.msg
		}
	}
	switch msg := msg.(type) {
	case configSavedMsg:
		a.saving = false
		a.configError = msg.err
		if len(a.edits) > 0 {
			return a, a.saveConfig()
		}
		return a, nil
	case screens.ConfigMsg:
		return a, a.editConfig(msg.Edit)
	}
	switch msg := msg.(type) {
	case systemAppearanceMsg:
		a.watching = false
		if theme.CurrentMode() != theme.Auto {
			return a, nil
		}
		theme.UpdateSystemAppearance(msg.dark)
		return a, a.watchAppearance()
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		return a, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			closePage(a.page)
			return a, tea.Quit
		}
		if msg.String() == "t" && !a.active().Busy() && !typing(a.active()) {
			return a, a.cycleTheme()
		}
	case newVersionMsg:
		a.newVersion = msg.version
		a.home.SetNewVersion(msg.version)
		return a, nil
	case animMsg:
		if a.anim == nil || msg.seq != a.anim.seq {
			return a, nil
		}
		if a.anim.progress() >= 1 {
			a.anim = nil
			return a, nil
		}
		return a, animTick(msg.seq)
	case screens.OpenMsg:
		cmd := a.open(msg.ID)
		// 启动时直接打开的工具不播放动画
		if a.start != "" {
			a.start = ""
			return a, cmd
		}
		return a, tea.Batch(cmd, a.animate(true))
	case screens.BackMsg:
		closePage(a.page)
		a.page = nil
		return a, tea.Batch(pageCommand(a.home, a.home.Refresh(a.pageID)), a.animate(false))
	case tea.QuitMsg:
		closePage(a.page)
		return a, tea.Quit
	case screens.RestartMsg:
		a.restart = true
		return a, tea.Quit
	}
	return a, pageCommand(a.active(), a.active().Update(msg))
}

func typing(p screens.Page) bool {
	t, ok := p.(screens.Typer)
	return ok && t.Typing()
}

// open 打开工具或自升级页
func (a *App) open(id string) tea.Cmd {
	if id == screens.SelfUpdateID {
		a.page, a.pageID = screens.NewSelfUpdate(a.env), id
		return pageCommand(a.page, a.page.Init())
	}
	t, ok := Find(id)
	if !ok {
		return nil
	}
	a.page, a.pageID = t.New(a.env), id
	a.home.Focus(id)
	return pageCommand(a.page, a.page.Init())
}

// animate 开始进入或退出工具页的过渡动画；动画关闭、窗口尺寸未知或涉及自升级页时不播放
func (a *App) animate(enter bool) tea.Cmd {
	a.anim = nil
	if a.env.Config.Animation == "off" || a.w == 0 || a.pageID == screens.SelfUpdateID {
		return nil
	}
	a.animSeq++
	a.anim = &transition{enter: enter, start: time.Now(), dur: animDuration, seq: a.animSeq}
	return animTick(a.animSeq)
}

func animTick(seq int) tea.Cmd {
	return tea.Tick(animStep, func(time.Time) tea.Msg { return animMsg{seq} })
}

// cycleTheme 按 自动 → 浅色 → 深色 切换主题
func (a *App) cycleTheme() tea.Cmd {
	next := theme.NextMode().String()
	return a.editConfig(func(c *config.Config) { c.Theme = next })
}

// editConfig 立即修改运行中的配置，写配置文件放到后台；前一次写入未完成时排队，按顺序写回。
// 修改涉及主题时同步切换，保证本次按键后的重绘就用上新主题
func (a *App) editConfig(edit func(*config.Config)) tea.Cmd {
	edit(&a.env.Config)
	var cmds []tea.Cmd
	// 在空配置上试跑一次，判断这次修改是否写了主题字段
	var probe config.Config
	edit(&probe)
	if mode, err := theme.ParseMode(probe.Theme); probe.Theme != "" && err == nil {
		theme.SetMode(mode)
		cmds = append(cmds, a.watchAppearance())
	}
	a.edits = append(a.edits, edit)
	if !a.saving {
		cmds = append(cmds, a.saveConfig())
	}
	return tea.Batch(cmds...)
}

func (a *App) watchAppearance() tea.Cmd {
	if theme.CurrentMode() != theme.Auto || a.watching {
		return nil
	}
	a.watching = true
	return watchSystemAppearance()
}

// saveConfig 读取配置文件，应用排队的修改后写回；读取失败时不写，避免覆盖用户手动改坏的文件
func (a *App) saveConfig() tea.Cmd {
	edits := a.edits
	a.edits, a.saving = nil, true
	return func() tea.Msg {
		c, err := config.Load()
		if err == nil {
			for _, edit := range edits {
				edit(&c)
			}
			err = config.Save(c)
		}
		return configSavedMsg{err}
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
	status := p.Status()
	if a.configError != nil {
		status = theme.Fg(theme.Amber).Render("设置未保存：配置文件读取或写入失败")
	}
	foot := widget.Footer(a.w, p.Hints(), status)
	bodyH := max(1, a.h-lipgloss.Height(head)-lipgloss.Height(foot))
	return widget.Frame(a.w, a.h, head, a.body(p, bodyH), foot)
}

// body 页面主体。工具页顶部显示收起后的分类条，窗口太矮时省略；过渡动画期间按帧渲染
func (a *App) body(p screens.Page, h int) string {
	if h < 20 || a.page != nil && a.pageID == screens.SelfUpdateID {
		return p.Body(a.w, h)
	}
	if an := a.anim; an != nil && (a.page != nil) == an.enter {
		if an.slide == nil || an.slide.W != a.w || an.slide.H != h {
			an.slide = a.home.NewSlide(a.w, h, an.enter, p.Body)
			an.dur = an.slide.Duration(animStep)
		}
		return an.slide.Frame(an.progress())
	}
	if a.page == nil {
		return p.Body(a.w, h)
	}
	strip := a.home.Strip(a.w)
	return strip + "\n" + p.Body(a.w, h-lipgloss.Height(strip))
}
