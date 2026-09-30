package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// 模拟系统切换消息，验证首页的正文与按键底色立即刷新且检测持续运行。
func TestSystemAppearanceRefreshesView(t *testing.T) {
	oldMode, oldProfile := theme.CurrentMode(), lipgloss.ColorProfile()
	t.Cleanup(func() {
		theme.SetMode(oldMode)
		lipgloss.SetColorProfile(oldProfile)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme.SetMode(theme.Auto)
	app := New(screens.Env{}, "")
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(systemAppearanceMsg{dark: true})
	darkView := app.View()
	_, cmd := app.Update(systemAppearanceMsg{dark: false})
	lightView := app.View()
	if cmd == nil {
		t.Fatal("外观变化后必须继续检测")
	}
	if darkView == lightView {
		t.Fatal("外观变化后页面没有重绘新配色")
	}
	// 使用显式浅色构造期望值，避免颜色转换时的舍入差异影响断言。
	wantKey := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text.Light)).
		Background(lipgloss.Color(theme.Surface.Light)).Padding(0, 1).Render("enter")
	if !strings.Contains(lightView, wantKey) {
		t.Fatal("浅色页面的按键文字或底色仍在使用旧配色")
	}
	app.Update(systemAppearanceMsg{dark: true})
	if app.View() != darkView {
		t.Fatal("切回深色后页面配色未恢复")
	}
}

// 已关闭页面的迟到消息不能交给当前页面
func TestStalePageMessageDropped(t *testing.T) {
	app := New(screens.Env{}, "")
	old := screens.NewErrorPage(nil, nil)
	app.page = screens.NewErrorPage(nil, nil)
	_, cmd := app.Update(pageMsg{page: old, msg: screens.BackMsg{}})
	if cmd != nil || app.page == nil {
		t.Fatal("旧页面的消息不应生效")
	}
}

// 固定主题下停止轮询系统外观
func TestFixedThemeStopsPolling(t *testing.T) {
	old := theme.CurrentMode()
	t.Cleanup(func() { theme.SetMode(old) })
	theme.SetMode(theme.Dark)
	app := New(screens.Env{}, "")
	if _, cmd := app.Update(systemAppearanceMsg{dark: true}); cmd != nil {
		t.Fatal("固定主题不应继续检测系统外观")
	}
}

// 配置文件损坏时切换主题不能覆盖写回
func TestThemeSaveKeepsCorruptConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	p := filepath.Join(dir, "sysbox", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []byte(`{"projects": {"roots": ["~/code"]`)
	if err := os.WriteFile(p, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	app := New(screens.Env{}, "")
	app.edits = []func(*config.Config){func(c *config.Config) { c.Theme = "dark" }}
	msg := app.saveConfig()()
	if saved, ok := msg.(configSavedMsg); !ok || saved.err == nil {
		t.Fatalf("应报告读取失败：%#v", msg)
	}
	if got, _ := os.ReadFile(p); string(got) != string(bad) {
		t.Fatal("损坏的配置被覆盖")
	}
}

// 首页修改设置后同步运行中的配置并写回文件；写主题字段时立即切换主题
func TestConfigEditSavesAndSwitchesTheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	old := theme.CurrentMode()
	t.Cleanup(func() { theme.SetMode(old) })
	theme.SetMode(theme.Light)

	app := New(screens.Env{}, "")
	save := app.editConfig(func(c *config.Config) { c.Claude.Direct = true })
	if !app.env.Config.Claude.Direct || theme.CurrentMode() != theme.Light {
		t.Fatal("非主题的修改应只更新配置，不改变主题")
	}
	app.editConfig(func(c *config.Config) { c.Theme = "dark" })
	if theme.CurrentMode() != theme.Dark || len(app.edits) != 1 {
		t.Fatalf("主题应立即切换，写入中的修改应排队：%d", len(app.edits))
	}
	for msg := tea.Msg(save()); msg != nil; {
		_, cmd := app.Update(msg)
		msg = nil
		if cmd != nil {
			msg = cmd()
		}
	}
	c, err := config.Load()
	if err != nil || !c.Claude.Direct || c.Theme != "dark" {
		t.Fatalf("两次修改都应写回配置文件：%+v %v", c, err)
	}
}

// 打开工具与返回首页都播放过渡动画，逐帧推进后结束；过期的帧消息被丢弃
func TestTransitionAnimation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := New(screens.Env{DryRun: true}, "")
	app.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	app.Update(screens.OpenMsg{ID: "claude"})
	if app.anim == nil || !app.anim.enter {
		t.Fatal("打开工具应开始进入动画")
	}
	seq := app.anim.seq
	if app.View() == "" || app.anim.slide == nil {
		t.Fatal("动画首帧应生成过渡画面")
	}
	app.Update(animMsg{seq})
	if app.anim == nil {
		t.Fatal("动画不应提前结束")
	}
	app.anim.start = time.Now().Add(-app.anim.dur)
	app.Update(animMsg{seq - 1})
	if app.anim == nil {
		t.Fatal("过期的帧消息应被丢弃")
	}
	app.Update(animMsg{seq})
	if app.anim != nil {
		t.Fatal("播放时长用完后动画应结束")
	}
	if !strings.Contains(app.View(), "◆ 工具") {
		t.Fatal("工具页顶部应显示分类条并高亮所在分类")
	}
	app.Update(screens.BackMsg{})
	if app.anim == nil || app.anim.enter {
		t.Fatal("返回首页应开始退出动画")
	}
}

// 启动时直接打开的工具和关闭动画后都不播放
func TestTransitionSkipped(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := New(screens.Env{DryRun: true}, "claude")
	app.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	app.Update(screens.OpenMsg{ID: "claude"})
	if app.anim != nil {
		t.Fatal("启动时直接打开的工具不应播放动画")
	}
	app.Update(screens.BackMsg{})
	app.env.Config.Animation = "off"
	app.Update(screens.OpenMsg{ID: "claude"})
	if app.anim != nil {
		t.Fatal("关闭动画后不应播放")
	}
}
