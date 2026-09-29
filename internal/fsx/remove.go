package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
)

// ForceRemoveAll 删除目录树。Go 模块缓存、部分 node_modules 里有只读文件和目录，
// 普通删除失败时先补上写权限再删一次。不跟随符号链接。
func ForceRemoveAll(p string) error {
	if err := os.RemoveAll(p); err == nil {
		return nil
	}
	_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().Perm()&0o200 == 0 {
			_ = os.Chmod(q, info.Mode().Perm()|0o200)
		}
		return nil
	})
	return os.RemoveAll(p)
}
