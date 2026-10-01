package disk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func isLink(info fs.FileInfo) bool { return info.Mode()&fs.ModeSymlink != 0 }

func Volumes(ctx context.Context) ([]Volume, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("df", "-kP"))
	if err != nil {
		return nil, err
	}
	return decodeVolumes(out), nil
}

// 系统挂载在 /Volumes 下的隐藏卷
var systemVolumes = map[string]bool{"/Volumes/Recovery": true, "/Volumes/Preboot": true, "/Volumes/VM": true, "/Volumes/Update": true}

// decodeVolumes 只保留系统盘与外接卷；/System/Volumes 下的 VM、Preboot 等由系统管理，不单独展示
func decodeVolumes(out string) []Volume {
	var volumes []Volume
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		mount := strings.Join(fields[5:], " ")
		if mount != "/" && !strings.HasPrefix(mount, "/Volumes/") || systemVolumes[mount] {
			continue
		}
		total, e := strconv.ParseInt(fields[1], 10, 64)
		if e != nil {
			continue
		}
		free, e := strconv.ParseInt(fields[3], 10, 64)
		if e != nil {
			continue
		}
		name := filepath.Base(mount)
		if mount == "/" {
			name = "系统磁盘"
		}
		volumes = append(volumes, Volume{Name: name, Path: mount, Total: total * 1024, Free: free * 1024})
	}
	return volumes
}

// Trash 移到当前用户的废纸篓，重名时追加序号；跨卷移动会失败并给出原因
func Trash(_ context.Context, path string) error {
	if err := Trashable(path); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trash := filepath.Join(home, ".Trash")
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dest := filepath.Join(trash, base)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dest = filepath.Join(trash, fmt.Sprintf("%s %d%s", stem, i, ext))
	}
	if err := os.Rename(path, dest); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return fmt.Errorf("文件不在系统磁盘上，无法移到废纸篓")
		}
		return err
	}
	return nil
}
