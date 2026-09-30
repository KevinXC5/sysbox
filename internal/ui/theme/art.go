package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// logoGlyphs ANSI Shadow 字体的 SYSBOX 字形，每个字母 6 行
var logoGlyphs = map[rune][]string{
	'S': {"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},
	'Y': {"██╗   ██╗", "╚██╗ ██╔╝", " ╚████╔╝ ", "  ╚██╔╝  ", "   ██║   ", "   ╚═╝   "},
	'B': {"██████╗ ", "██╔══██╗", "██████╔╝", "██╔══██╗", "██████╔╝", "╚═════╝ "},
	'O': {" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	'X': {"██╗  ██╗", "╚██╗██╔╝", " ╚███╔╝ ", " ██╔██╗ ", "██╔╝ ██╗", "╚═╝  ╚═╝"},
}

// Logo 渲染带品牌渐变的 SYSBOX 大字
func Logo() string {
	return BrandGradient(assemble("SYSBOX", logoGlyphs, 6, ""))
}

// pixelColors 像素图标的调色板，位图里的每个字母对应一种颜色；'.' 与空格表示透明。
// 浅色主题下亮色整体压暗，保证白底上仍看得清轮廓
var pixelColors = map[rune]lipgloss.AdaptiveColor{
	'p': ac("#7C3AED", "#A78BFA"), // 紫
	'P': ac("#A78BFA", "#DDD6FE"), // 浅紫
	'c': ac("#0891B2", "#22D3EE"), // 青
	'C': ac("#22D3EE", "#A5F3FC"), // 浅青
	'b': ac("#2563EB", "#60A5FA"), // 蓝
	'B': ac("#60A5FA", "#BFDBFE"), // 浅蓝
	'g': ac("#059669", "#34D399"), // 绿
	'y': ac("#D97706", "#FBBF24"), // 琥珀
	'Y': ac("#F59E0B", "#FDE68A"), // 浅琥珀
	'o': ac("#C2410C", "#FB923C"), // 橙
	'n': ac("#78350F", "#B45309"), // 木色
	'r': ac("#E11D48", "#FB7185"), // 玫红
	'R': ac("#FB7185", "#FECDD3"), // 浅玫红
	'x': ac("#9F1239", "#BE123C"), // 深红
	'w': ac("#CBD5E1", "#F1F5F9"), // 白
	's': ac("#475569", "#64748B"), // 深石板
	'S': ac("#64748B", "#94A3B8"), // 石板
	'K': ac("#0F172A", "#020617"), // 黑
}

// PixelArt 把彩色位图渲染成字符画：每个字符格用 ▀ 表示上下两个像素，前景色画上半、背景色画下半，
// 透明像素露出 base 的底色。dim 为 true 时颜色向弱化色靠拢，用于未选中的状态。
// 位图行数为奇数时，最后一行的下半按透明处理
func PixelArt(rows []string, base lipgloss.Style, dim bool) string {
	color := func(r rune) (lipgloss.Color, bool) {
		c, ok := pixelColors[r]
		if !ok {
			return "", false
		}
		if !dim {
			return lipgloss.Color(Hex(c)), true
		}
		return lerp(parseHex(Hex(c)), parseHex(Hex(Muted)), 0.6), true
	}
	var out []string
	for i := 0; i < len(rows); i += 2 {
		top, bot := []rune(rows[i]), []rune{}
		if i+1 < len(rows) {
			bot = []rune(rows[i+1])
		}
		var sb strings.Builder
		for j := range max(len(top), len(bot)) {
			var t, b lipgloss.Color
			var tok, bok bool
			if j < len(top) {
				t, tok = color(top[j])
			}
			if j < len(bot) {
				b, bok = color(bot[j])
			}
			switch {
			case tok && bok && t == b:
				sb.WriteString(base.Foreground(t).Render("█"))
			case tok && bok:
				sb.WriteString(lipgloss.NewStyle().Foreground(t).Background(b).Render("▀"))
			case tok:
				sb.WriteString(base.Foreground(t).Render("▀"))
			case bok:
				sb.WriteString(base.Foreground(b).Render("▄"))
			default:
				sb.WriteString(base.Render(" "))
			}
		}
		out = append(out, sb.String())
	}
	return strings.Join(out, "\n")
}

// digitGlyphs 3 行高的块状数字字体，用于结果页的大号数值
var digitGlyphs = map[rune][]string{
	'0': {"█▀█", "█ █", "▀▀▀"},
	'1': {"▀█ ", " █ ", "▀▀▀"},
	'2': {"▀▀█", "█▀▀", "▀▀▀"},
	'3': {"▀▀█", " ▀█", "▀▀▀"},
	'4': {"█ █", "▀▀█", "  ▀"},
	'5': {"█▀▀", "▀▀█", "▀▀▀"},
	'6': {"█▀▀", "█▀█", "▀▀▀"},
	'7': {"▀▀█", "  █", "  ▀"},
	'8': {"█▀█", "█▀█", "▀▀▀"},
	'9': {"█▀█", "▀▀█", "▀▀▀"},
	'.': {" ", " ", "▀"},
}

// BigNumber 渲染 3 行高的品牌渐变大号数字，只支持数字和小数点
func BigNumber(s string) string {
	return BrandGradient(assemble(s, digitGlyphs, 3, " "))
}

// assemble 把字符逐个拼成多行字形
func assemble(s string, glyphs map[rune][]string, rows int, sep string) string {
	lines := make([]string, rows)
	for i, r := range s {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for row := range rows {
			if i > 0 {
				lines[row] += sep
			}
			lines[row] += g[row]
		}
	}
	return strings.Join(lines, "\n")
}
