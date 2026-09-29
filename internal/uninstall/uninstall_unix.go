//go:build !windows

package uninstall

import "os"

// userPathHas macOS 的安装脚本不修改 PATH，没有需要撤销的内容
func userPathHas(string) bool { return false }

func removeUserPath(string) error { return nil }

// removeExecutable Unix 上可以直接删除正在运行的程序文件
func removeExecutable(exe string) error { return os.Remove(exe) }
