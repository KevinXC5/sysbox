//go:build windows

package fsx

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// AllocSize Windows 上取不到已分配块数，按文件逻辑大小计算
func AllocSize(info fs.FileInfo) int64 { return info.Size() }

// Identify 读取路径自身（不跟随符号链接与 junction）的卷序列号与文件索引
func Identify(p string) (FileID, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return FileID{}, err
	}
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return FileID{}, err
	}
	defer windows.CloseHandle(h)
	var d windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &d); err != nil {
		return FileID{}, err
	}
	return FileID{Dev: uint64(d.VolumeSerialNumber), Ino: uint64(d.FileIndexHigh)<<32 | uint64(d.FileIndexLow)}, nil
}
