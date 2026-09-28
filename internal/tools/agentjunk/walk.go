package agentjunk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/fsx"
)

// errForeignLink 目录内出现了不能安全处理的符号链接
var errForeignLink = errors.New("目录内含符号链接")

// usage 统计条目占用的磁盘块和最近修改时间，不跟随链接。
// allowInternalLinks 为真时允许指向条目自身内部的链接（原生安装包常见），否则遇到链接即报错。
func usage(item string, allowInternalLinks bool) (size int64, latest time.Time, err error) {
	info, err := os.Lstat(item)
	if err != nil {
		return 0, time.Time{}, err
	}
	size, latest = fsx.AllocSize(info), info.ModTime()
	if !info.IsDir() {
		return size, latest, nil
	}
	realItem, err := filepath.EvalSymlinks(item)
	if err != nil {
		return 0, time.Time{}, err
	}
	err = filepath.WalkDir(item, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == item {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			if !allowInternalLinks {
				return errForeignLink
			}
			target, err := filepath.EvalSymlinks(p)
			if err != nil || !within(target, realItem) {
				return errForeignLink
			}
		}
		size += fsx.AllocSize(fi)
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
		return nil
	})
	return size, latest, err
}

// identity 文件标识，删除前用来确认目标没有被替换
type identity = fsx.FileID

func identify(p string) (identity, error) { return fsx.Identify(p) }

// plainDir 路径是真实目录，且自身及各级父目录都不是符号链接。
// 不能拿 EvalSymlinks 的结果和原路径做字符串比较：Windows 上它会把 8.3 短路径
// （os.TempDir 常见的 C:\Users\RUNNER~1\...）展开成长路径，并统一盘符大小写，
// 真实目录也会被判成“含链接”，整组扫描因此被跳过。
func plainDir(p string) bool {
	p = filepath.Clean(p)
	if p == "" || p == "." {
		return false
	}
	vol := filepath.VolumeName(p)
	for cur := p; len(cur) > len(vol); {
		fi, err := os.Lstat(cur)
		if err != nil || !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
			return false
		}
		next := filepath.Dir(cur)
		if next == cur {
			break
		}
		cur = next
	}
	return true
}

// samePath 判断两条路径是否指向同一位置。Windows 忽略大小写，分隔符统一后再比。
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return false
}

// isSymlink 路径本身是否为符号链接
func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// within 判断 p 是否等于 root 或位于其下。Windows 路径大小写不敏感。
func within(p, root string) bool {
	sep := string(filepath.Separator)
	if goos == "windows" {
		p, root = strings.ToLower(filepath.Clean(p)), strings.ToLower(filepath.Clean(root))
	}
	return p == root || strings.HasPrefix(p, root+sep)
}
