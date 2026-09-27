package theme

import "testing"

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
