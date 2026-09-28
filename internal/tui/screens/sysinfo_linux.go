package screens

import (
	"os"
	"strings"
	"syscall"
)

func readSysInfo() sysInfo {
	var info sysInfo
	info.os = osRelease()
	// 统计家目录所在分区，用户数据通常在这里
	path := "/"
	if home, err := os.UserHomeDir(); err == nil {
		path = home
	}
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) == nil {
		info.free = int64(st.Bavail) * int64(st.Bsize)
		info.total = int64(st.Blocks) * int64(st.Bsize)
	}
	return info
}

// osRelease 取 /etc/os-release 里的发行版名称，取不到时返回 Linux
func osRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return "Linux"
}
