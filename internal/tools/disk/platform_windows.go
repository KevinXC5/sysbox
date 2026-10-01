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

const psPrefix = `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false); `

func Volumes(ctx context.Context) ([]Volume, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psPrefix+`ConvertTo-Json -InputObject @(Get-CimInstance Win32_LogicalDisk | Where-Object { $_.Size -gt 0 } | ForEach-Object { [pscustomobject]@{Name=[string]$_.VolumeName; Path=[string]$_.DeviceID+'\'; Total=[int64]$_.Size; Free=[int64]$_.FreeSpace} }) -Compress`))
	if err != nil {
		return nil, err
	}
	var volumes []Volume
	err = json.Unmarshal([]byte(strings.TrimPrefix(out, "\ufeff")), &volumes)
	for i := range volumes {
		if volumes[i].Name == "" {
			volumes[i].Name = "本地磁盘"
		}
	}
	return volumes, err
}

// Trash 通过系统接口移到回收站，可在回收站中还原
func Trash(ctx context.Context, path string) error {
	if err := Trashable(path); err != nil {
		return err
	}
	quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	script := psPrefix + `Add-Type -AssemblyName Microsoft.VisualBasic; $p=` + quoted + `; if(Test-Path -LiteralPath $p -PathType Container){ [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($p,'OnlyErrorDialogs','SendToRecycleBin') } else { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($p,'OnlyErrorDialogs','SendToRecycleBin') }`
	_, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script))
	return err
}
