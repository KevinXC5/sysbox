package screens

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func readSysInfo() sysInfo {
	var info sysInfo
	v := windows.RtlGetVersion()
	// Windows 11 的主版本号仍是 10，只能按构建号区分
	name := "Windows 10"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	}
	info.os = fmt.Sprintf("%s (%d)", name, v.BuildNumber)
	// 统计家目录所在的盘
	root := `C:\`
	if home, err := os.UserHomeDir(); err == nil {
		root = filepath.VolumeName(home) + `\`
	}
	if p, err := windows.UTF16PtrFromString(root); err == nil {
		var free, total uint64
		if windows.GetDiskFreeSpaceEx(p, &free, &total, nil) == nil {
			info.free, info.total = int64(free), int64(total)
		}
	}
	return info
}
