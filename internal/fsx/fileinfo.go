package fsx

// FileID 文件在所属卷上的唯一标识：Unix 为设备号与 inode，Windows 为卷序列号与文件索引
type FileID struct{ Dev, Ino uint64 }
