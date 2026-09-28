//go:build windows

package obsidianlink

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// linkDir 创建目录 junction。
// Windows 上 os.Symlink 指向目录需要管理员或开发者模式；junction 普通用户即可创建。
// 用 cmd 的 mklink /J，不额外引入依赖。
func linkDir(target, link string) error {
	// mklink 是 cmd 内建命令。路径直接拼进 /c 的命令字符串并加引号，
	// 避免含空格时被拆成多个参数。
	script := "mklink /J " + cmdQuote(link) + " " + cmdQuote(target)
	out, err := exec.Command("cmd", "/c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建 junction 失败：%w：%s", err, out)
	}
	return nil
}

// isLink 符号链接，或 Go 1.23+ 报成 ModeIrregular 的目录 junction。
func isLink(fi os.FileInfo) bool {
	mode := fi.Mode()
	return mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0
}

// cmdQuote 给 cmd.exe 的参数加引号。cmd 里引号用双写转义。
func cmdQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
