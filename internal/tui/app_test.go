package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	msg := app.saveTheme("dark")()
	if saved, ok := msg.(themeSavedMsg); !ok || saved.err == nil {
		t.Fatalf("应报告读取失败：%#v", msg)
	}
	if got, _ := os.ReadFile(p); string(got) != string(bad) {
		t.Fatal("损坏的配置被覆盖")
	}
}
