package tui

import (
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
