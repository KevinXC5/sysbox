package theme

import (
	"github.com/charmbracelet/lipgloss"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{"auto": Auto, "Light": Light, " dark ": Dark}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v；期望 %v", in, got, err, want)
		}
	}
	if _, err := ParseMode("blue"); err == nil {
		t.Error("未知模式应返回错误")
	}
}

// 切换顺序为 自动 → 浅色 → 深色 → 自动，固定模式不受自动判定影响
func TestModeCycle(t *testing.T) {
	autoDark = true
	SetMode(Auto)
	for _, want := range []Mode{Light, Dark, Auto} {
		SetMode(NextMode())
		if CurrentMode() != want {
			t.Fatalf("期望切换到 %v，实际 %v", want, CurrentMode())
		}
	}
	SetMode(Light)
	if IsDark() {
		t.Error("浅色模式下 IsDark 应为 false")
	}
	SetMode(Auto)
	if !IsDark() {
		t.Error("自动模式下应沿用判定结果")
	}
}

// 自动模式跟随系统双向切换，固定模式不变，切回自动时使用最新外观。
func TestSystemAppearanceChanges(t *testing.T) {
	oldMode, oldAuto, oldSystem := mode, autoDark, lastSystemDark
	t.Cleanup(func() {
		autoDark, lastSystemDark = oldAuto, oldSystem
		SetMode(oldMode)
	})
	autoDark, lastSystemDark = true, true
	SetMode(Auto)
	for _, dark := range []bool{false, true, false} {
		UpdateSystemAppearance(dark)
		if IsDark() != dark || lipgloss.HasDarkBackground() != dark {
			t.Fatalf("自动配色未跟随系统外观：深色=%v", dark)
		}
		if Hex(Text) != map[bool]string{false: Text.Light, true: Text.Dark}[dark] {
			t.Fatal("渐变与普通文字未使用同一套配色")
		}
	}
	for _, fixed := range []Mode{Light, Dark} {
		SetMode(fixed)
		UpdateSystemAppearance(true)
		UpdateSystemAppearance(false)
		if IsDark() != (fixed == Dark) || lipgloss.HasDarkBackground() != (fixed == Dark) {
			t.Fatalf("系统外观覆盖了固定主题 %v", fixed)
		}
		SetMode(Auto)
		if IsDark() {
			t.Fatal("切回自动后未采用最新系统外观")
		}
	}
}

// 终端可以采用独立主题，系统外观不变时不能覆盖启动时检测的背景。
func TestUnchangedSystemAppearancePreservesTerminalTheme(t *testing.T) {
	oldMode, oldAuto, oldSystem := mode, autoDark, lastSystemDark
	t.Cleanup(func() {
		autoDark, lastSystemDark = oldAuto, oldSystem
		SetMode(oldMode)
	})
	autoDark, lastSystemDark = true, false
	SetMode(Auto)
	UpdateSystemAppearance(false)
	if !IsDark() || !lipgloss.HasDarkBackground() {
		t.Fatal("未发生系统切换时应保留终端的独立深色配色")
	}
}
