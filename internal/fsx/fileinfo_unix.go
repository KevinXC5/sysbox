//go:build !windows

package fsx

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// AllocSize 优先按已分配块计算，稀疏文件和 APFS 压缩文件才不会被高估。
func AllocSize(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}

// Identify 读取路径自身（不跟随符号链接）的设备号与 inode
func Identify(p string) (FileID, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return FileID{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, errors.New("无法读取文件标识")
	}
	return FileID{Dev: uint64(st.Dev), Ino: st.Ino}, nil
}
