//go:build !windows

package obsidianlink

import (
	"os"
	"testing"
)

func TestIrregularIsNotLink(t *testing.T) {
	if isLink(fakeInfo{os.ModeIrregular}) {
		t.Fatal("非 Windows 的 ModeIrregular 不是链接")
	}
}
