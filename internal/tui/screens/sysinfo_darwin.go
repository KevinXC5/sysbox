package screens

import (
	"os/exec"
	"strings"
	"syscall"
)

func readSysInfo() sysInfo {
	var info sysInfo
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		info.os = "macOS " + strings.TrimSpace(string(out))
	}
	// APFS 下用户数据在 Data 卷，取不到时退回根卷
	var st syscall.Statfs_t
	if syscall.Statfs("/System/Volumes/Data", &st) == nil || syscall.Statfs("/", &st) == nil {
		info.free = int64(st.Bavail) * int64(st.Bsize)
		info.total = int64(st.Blocks) * int64(st.Bsize)
	}
	return info
}
