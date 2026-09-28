//go:build windows

package obsidianlink

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// linkDir 创建目录 junction。
// Windows 上 os.Symlink 指向目录需要管理员或开发者模式；junction 普通用户即可创建。
// 用 cmd 的 mklink /J，不额外引入依赖。
func linkDir(target, link string) error {
	// mklink 是 cmd 内建命令。Go 默认按 C 运行时规则转义参数（引号写成 \"），
	// cmd 不认这种写法，所以直接给出完整命令行，路径各自加引号以支持空格。
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: "cmd.exe /d /c mklink /J " + cmdQuote(link) + " " + cmdQuote(target),
	}
	out, err := cmd.CombinedOutput()
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
