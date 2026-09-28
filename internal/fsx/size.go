// Package fsx 提供文件系统相关的通用工具：磁盘占用统计、大小格式化、路径美化。
package fsx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DiskUsage 统计路径实际占用的磁盘空间（字节），口径与 du 一致。
// 不跟随符号链接；遇到无权限等错误时跳过该条目，不中断统计。
func DiskUsage(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += AllocSize(info)
		return nil
	})
	return total
}

// 大小单位，按 1024 进制
var units = []string{"B", "KB", "MB", "GB", "TB"}

// SplitBytes 把字节数拆成数值和单位两部分，便于界面分开排版。
func SplitBytes(b int64) (string, string) {
	if b < 1024 {
		return fmt.Sprintf("%d", b), "B"
	}
	v := float64(b)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	// 数值越大小数位越少，保持整体宽度稳定
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v), units[i]
	case v >= 10:
		return fmt.Sprintf("%.1f", v), units[i]
	default:
		return fmt.Sprintf("%.2f", v), units[i]
	}
}

// FormatBytes 返回“217 MB”这样的可读字符串。
func FormatBytes(b int64) string {
	n, u := SplitBytes(b)
	return n + " " + u
}

// PrettyPath 把家目录前缀替换成 ~，缩短展示长度。
func PrettyPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
