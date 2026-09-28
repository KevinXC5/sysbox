//go:build windows

package selfupdate

import (
	"fmt"
	"os"
)

// oldSuffix 被替换下来的旧可执行文件后缀。Windows 不允许覆盖或删除正在运行的 exe，
// 所以先把它改名为同目录下的 .old，新文件再落到原路径；下次启动时由 CleanupOld 清理。
const oldSuffix = ".old"

// replaceExecutable 先把正在运行的 exe 改名为 exe.old，再把新文件放到原路径。
// 任一步失败都尽量把旧文件改回去，避免留下无法启动的安装。
func replaceExecutable(tmp, exe string) error {
	old := exe + oldSuffix
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("无法移开正在运行的程序：%w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		if back := os.Rename(old, exe); back != nil {
			return fmt.Errorf("安装新版本失败（%w），且无法恢复旧版本：%v", err, back)
		}
		return fmt.Errorf("安装新版本失败，已保留当前版本：%w", err)
	}
	return nil
}
