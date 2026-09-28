package screens

import (
	"fmt"
	"testing"
)

func TestWrapText(t *testing.T) {
	cases := []struct {
		s    string
		w    int
		want []string
	}{
		// 中文紧跟英文单词时按字折行，不整段挪到下一行
		{"JetBrains 缓存清理会按系统定位", 16, []string{"JetBrains 缓存清", "理会按系统定位"}},
		// 英文单词不在中间断开
		{"识别 Windows，下载", 10, []string{"识别", "Windows，", "下载"}},
		// 中文标点不出现在行首
		{"一二三四，五", 8, []string{"一二三", "四，五"}},
		// 超长单词硬折断
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"", 10, []string{""}},
	}
	for _, c := range cases {
		if got := wrapText(c.s, c.w); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("wrapText(%q, %d) = %q，期望 %q", c.s, c.w, got, c.want)
		}
	}
}
