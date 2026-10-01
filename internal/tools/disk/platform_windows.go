package disk

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"syscall"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func isLink(info fs.FileInfo) bool {
	if info.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}

func Volumes(ctx context.Context) ([]Volume, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false); ConvertTo-Json -InputObject @(Get-CimInstance Win32_LogicalDisk | Where-Object { $_.Size -gt 0 } | ForEach-Object { [pscustomobject]@{Name=[string]$_.VolumeName; Path=[string]$_.DeviceID+'\'; Total=[int64]$_.Size; Free=[int64]$_.FreeSpace} }) -Compress`))
	if err != nil {
		return nil, err
	}
	var volumes []Volume
	err = json.Unmarshal([]byte(strings.TrimPrefix(out, "\ufeff")), &volumes)
	return volumes, err
}
