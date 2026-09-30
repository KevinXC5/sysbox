// Package fsx 提供文件系统相关的通用工具：磁盘占用统计、大小格式化、路径美化。
package fsx

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Usage 一次磁盘占用统计的结果
type Usage struct {
	Bytes   int64
	Skipped int // 因无权限等错误跳过的条目数，非零时 Bytes 可能偏小
}

// Partial 统计是否不完整
func (u Usage) Partial() bool { return u.Skipped > 0 }

// DiskUsageContext 统计路径实际占用的磁盘空间，口径与 du 一致。
// 不跟随符号链接；遇到无权限等错误时跳过该条目并计数，不中断统计；取消后立即停止遍历。
func DiskUsageContext(ctx context.Context, path string) Usage {
	var u Usage
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			u.Skipped++
			return nil
		}
		info, err := d.Info()
		if err != nil {
			u.Skipped++
			return nil
		}
		u.Bytes += AllocSize(info)
		return nil
	})
	return u
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
