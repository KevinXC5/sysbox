//go:build windows

package uninstall

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// removalScript never embeds a filesystem path in executable source. Environment
// values are read as data, so %, quotes, $, and PowerShell metacharacters stay literal.
const removalScript = `$ErrorActionPreference = 'Stop'
$target = $env:SYSBOX_REMOVE_EXE
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ($true) {
        try {
            [System.IO.File]::Delete($target + '.old')
            [System.IO.File]::Delete($target)
            break
        } catch {
            if ([DateTime]::UtcNow -ge $deadline) { throw }
            Start-Sleep -Milliseconds 200
        }
    }
    $dir = [System.IO.Path]::GetDirectoryName($target)
    if ([System.IO.Directory]::Exists($dir) -and [System.IO.Directory]::GetFileSystemEntries($dir).Length -eq 0) {
        [System.IO.Directory]::Delete($dir, $false)
    }
    [Console]::WriteLine('sysbox uninstall completed')
    exit 0
} catch {
    [Console]::Error.WriteLine('sysbox uninstall failed: ' + $_.Exception.Message)
    exit 1
}`

func removalCommand(exe string) (*exec.Cmd, error) {
	if !filepath.IsAbs(exe) {
		return nil, fmt.Errorf("程序路径必须是绝对路径：%s", exe)
	}
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	powershell := filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", removalScript)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.EqualFold(key, "SYSBOX_REMOVE_EXE") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "SYSBOX_REMOVE_EXE="+exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd, nil
}

// removeExecutable removes ordinary files synchronously; the running executable
// needs a helper and reports pending instead of claiming successful removal.
func removeExecutable(exe string) error {
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	if err := os.Remove(exe + ".old"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除升级残留失败：%w", err)
	}
	if err := os.Remove(exe); err == nil {
		entries, err := os.ReadDir(filepath.Dir(exe))
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return os.Remove(filepath.Dir(exe))
		}
		return nil
	} else if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	cmd, err := removalCommand(exe)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	return ErrRemovalPending
}
