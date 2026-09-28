//go:build windows

package obsidianlink

import (
	"os"
	"testing"
)

func TestJunctionModeIsLink(t *testing.T) {
	if !isLink(fakeInfo{os.ModeIrregular}) {
		t.Fatal("Windows junction 以 ModeIrregular 表示，应识别为链接")
	}
}

func TestCmdQuote(t *testing.T) {
	if got := cmdQuote(`C:\a b`); got != `"C:\a b"` {
		t.Fatalf("普通空格路径引号有误：%s", got)
	}
	if got := cmdQuote(`C:\a"b`); got != `"C:\a""b"` {
		t.Fatalf("内嵌引号应双写：%s", got)
	}
}
