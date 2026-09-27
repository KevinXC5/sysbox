// Package theme 定义 sysbox 的配色、通用样式与装饰性渲染（渐变、大号数字、Logo）。
// 所有颜色都有浅色、深色两套取值，渲染时按当前主题模式自动选用。
package theme

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Mode 主题模式
type Mode int

const (
	Auto  Mode = iota // 跟随终端背景色
	Light             // 固定浅色
	Dark              // 固定深色
)

var modeNames = [...]string{"auto", "light", "dark"}

func (m Mode) String() string { return modeNames[m] }

// Label 界面上显示的中文名
func (m Mode) Label() string {
	switch m {
	case Light:
		return "浅色"
	case Dark:
		return "深色"
	}
	return "自动"
}

// ParseMode 解析 auto / light / dark
func ParseMode(s string) (Mode, error) {
	for i, n := range modeNames {
		if strings.EqualFold(strings.TrimSpace(s), n) {
			return Mode(i), nil
		}
	}
	return Auto, fmt.Errorf("未知的主题模式 %q，可选 auto、light、dark", s)
}

var (
	mode           Mode
	autoDark       = true // 自动模式下判定的结果
	lastSystemDark bool   // 上次系统外观，用于保留终端启动时的独立配色
)

// Setup 初始化主题，必须在 Bubble Tea 接管终端之前调用。
// 自动模式通过查询终端背景色判定深浅；其他模式下不查询终端，
// 改用 macOS 外观设置作为之后切回自动模式时的依据。
func Setup(m Mode) {
	lastSystemDark = SystemDark()
	if m == Auto {
		autoDark = lipgloss.HasDarkBackground()
	} else {
		autoDark = lastSystemDark
	}
	SetMode(m)
}

// SetMode 切换主题模式，下一次渲染即生效
func SetMode(m Mode) {
	mode = m
	lipgloss.SetHasDarkBackground(IsDark())
}

// CurrentMode 当前主题模式
func CurrentMode() Mode { return mode }

// NextMode 按 自动 → 浅色 → 深色 循环
func NextMode() Mode { return (mode + 1) % 3 }

// IsDark 当前是否使用深色配色
func IsDark() bool {
	switch mode {
	case Light:
		return false
	case Dark:
		return true
	}
	return autoDark
}

// UpdateSystemAppearance 在界面消息循环中同步配色，避免后台检测与渲染并发修改主题。
// 仅在系统外观发生变化时覆盖启动时的终端判定，固定主题仍保留用户选择。
func UpdateSystemAppearance(dark bool) {
	if dark == lastSystemDark {
		return
	}
	lastSystemDark = dark
	autoDark = dark
	if mode == Auto {
		lipgloss.SetHasDarkBackground(dark)
	}
}

// SystemDark 读取 macOS 外观设置，浅色模式下该键不存在。
// 此函数只读取系统状态，可在后台调用。
func SystemDark() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Dark"
}

// ac 构造自适应颜色
func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// 调色板：品牌色为紫 → 青渐变；浅色主题整体压暗饱和度以保证白底上的对比度
var (
	Accent  = ac("#7C3AED", "#A78BFA") // 品牌主色：紫
	Accent2 = ac("#0891B2", "#22D3EE") // 品牌辅色：青
	Green   = ac("#059669", "#34D399")
	Amber   = ac("#C2410C", "#FBBF24")
	Blue    = ac("#2563EB", "#60A5FA")
	Slate   = ac("#64748B", "#94A3B8")
	Rose    = ac("#E11D48", "#FB7185")
	OnColor = ac("#FFFFFF", "#0B1020") // 彩色底上的文字

	Text    = ac("#1E293B", "#E2E8F0") // 正文
	Subtle  = ac("#475569", "#A3AED0") // 次要文字
	Muted   = ac("#8A96AB", "#5B6785") // 弱化文字
	Faint   = ac("#D5DCE6", "#2A3350") // 分割线、边框、空轨道
	Surface = ac("#E8ECF3", "#1A2138") // 按钮、按键底色
)

// Hex 返回自适应颜色在当前主题下的十六进制值
func Hex(c lipgloss.AdaptiveColor) string {
	if IsDark() {
		return c.Dark
	}
	return c.Light
}

// 常用文字样式
var (
	TextStyle   = lipgloss.NewStyle().Foreground(Text)
	BoldStyle   = lipgloss.NewStyle().Foreground(Text).Bold(true)
	SubtleStyle = lipgloss.NewStyle().Foreground(Subtle)
	MutedStyle  = lipgloss.NewStyle().Foreground(Muted)
	FaintStyle  = lipgloss.NewStyle().Foreground(Faint)
)

// Fg 返回指定前景色的样式
func Fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// Badge 彩色底的小标签
func Badge(label string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Foreground(OnColor).Background(bg).Bold(true).Padding(0, 1).Render(label)
}

// Key 按键提示里的按键块
func Key(k string) string {
	return lipgloss.NewStyle().Foreground(Text).Background(Surface).Padding(0, 1).Render(k)
}

// HintsFit 渲染底部按键提示条，按顺序放入，超出宽度的提示整条省略
func HintsFit(w int, pairs ...string) string {
	out := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		part := Key(pairs[i]) + " " + MutedStyle.Render(pairs[i+1])
		next := part
		if out != "" {
			next = out + "   " + part
		}
		if lipgloss.Width(next) > w {
			break
		}
		out = next
	}
	return out
}

// Rule 水平分割线
func Rule(w int) string {
	if w < 1 {
		return ""
	}
	return FaintStyle.Render(strings.Repeat("─", w))
}

// Truncate 按显示宽度截断，保留 ANSI 样式
func Truncate(s string, w int) string {
	return ansi.Truncate(s, w, "…")
}

// Gradient 按可见列把多行文本从 from 渐变到 to（十六进制），空格保持原样
func Gradient(s, from, to string) string {
	lines := strings.Split(s, "\n")
	maxW := 0
	for _, l := range lines {
		maxW = max(maxW, len([]rune(l)))
	}
	a, b := parseHex(from), parseHex(to)
	var out []string
	for _, l := range lines {
		var sb strings.Builder
		for i, r := range []rune(l) {
			if r == ' ' {
				sb.WriteRune(r)
				continue
			}
			t := 0.0
			if maxW > 1 {
				t = float64(i) / float64(maxW-1)
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(lerp(a, b, t)).Bold(true).Render(string(r)))
		}
		out = append(out, sb.String())
	}
	return strings.Join(out, "\n")
}

// BrandGradient 用当前主题的品牌渐变渲染文本
func BrandGradient(s string) string {
	return Gradient(s, Hex(Accent), Hex(Accent2))
}

type rgb struct{ r, g, b float64 }

func parseHex(h string) rgb {
	var r, g, b int
	_, _ = fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b)
	return rgb{float64(r), float64(g), float64(b)}
}

func lerp(a, b rgb, t float64) lipgloss.Color {
	c := func(x, y float64) int { return int(x + (y-x)*t + 0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c(a.r, b.r), c(a.g, b.g), c(a.b, b.b)))
}
