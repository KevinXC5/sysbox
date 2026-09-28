//go:build !windows

package selfupdate

// CleanupOld 清理上次升级留下的旧可执行文件。Unix 替换是原子的，没有残留文件。
func CleanupOld() {}
