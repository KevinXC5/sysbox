//go:build windows

package uninstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// envKey 用户级环境变量所在的注册表项，install.ps1 写入的 PATH 就在这里
const envKey = `Environment`

// userPathHas 用户 PATH 中是否包含 dir
func userPathHas(dir string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return false
	}
	_, ok := removeEntry(v, dir)
	return ok
}

// removeUserPath 从用户 PATH 中只移除 dir 这一项，保留原值类型（REG_EXPAND_SZ 中的 %VAR% 不被展开），
// 再广播环境变量变更，让之后新开的终端读到新值
func removeUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}
	rest, ok := removeEntry(v, dir)
	if !ok {
		return nil
	}
	if typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", rest)
	} else {
		err = k.SetStringValue("Path", rest)
	}
	if err != nil {
		return err
	}
	broadcastEnvChange()
	return nil
}

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcastEnvChange 通知资源管理器等进程环境变量已变更，失败不影响卸载结果
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

// removeExecutable Windows 不能删除正在运行的 exe，交给一个隐藏的 cmd 等当前进程退出后再删，
// 同时删除升级残留的 .old；安装目录因此变空时一并移除（rmdir 不删非空目录，不会误删其他文件）
func removeExecutable(exe string) error {
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	script := fmt.Sprintf(`ping -n 3 127.0.0.1 >nul & del /f /q "%s" "%s.old" 2>nul & rmdir "%s" 2>nul`,
		exe, exe, filepath.Dir(exe))
	cmd := exec.Command(comspec)
	// cmd 的引号规则与 Go 默认的参数转义不兼容，直接给出完整命令行；/c 后整体加一层引号，由 cmd 剥掉
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       fmt.Sprintf(`"%s" /d /c "%s"`, comspec, script),
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
