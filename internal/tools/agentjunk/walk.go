package agentjunk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
	size, latest = blocks(info), info.ModTime()
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
		size += blocks(fi)
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
		return nil
	})
	return size, latest, err
}

func blocks(fi fs.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}

// identity 设备号与 inode，删除前用来确认目标没有被替换
type identity struct{ dev, ino uint64 }

func identify(p string) (identity, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return identity{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return identity{}, errors.New("无法读取文件标识")
	}
	return identity{uint64(st.Dev), st.Ino}, nil
}

// plainDir 路径是真实目录，且自身及各级父目录都不是符号链接
func plainDir(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil || !fi.IsDir() {
		return false
	}
	real, err := filepath.EvalSymlinks(p)
	return err == nil && real == p
}

// isSymlink 路径本身是否为符号链接
func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// within 判断 p 是否等于 root 或位于其下
func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
