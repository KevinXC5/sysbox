//go:build !windows

package obsidianlink

import "os"

// linkDir 为子目录建立符号链接。非 Windows 不需要额外权限。
func linkDir(target, link string) error {
	return os.Symlink(target, link)
}

// isLink 只认符号链接。ModeIrregular 在 Unix 上不是链接，不能算进去。
func isLink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}
