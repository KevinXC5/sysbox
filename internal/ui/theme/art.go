package theme

import "strings"

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
