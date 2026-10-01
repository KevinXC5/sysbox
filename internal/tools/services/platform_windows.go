package services

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

const psPrefix = `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false); `

func ps(script string) sysx.Cmd {
	return sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psPrefix+script)
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

const serviceScript = `ConvertTo-Json -InputObject @(Get-CimInstance Win32_Service | ForEach-Object { [pscustomobject]@{ID=[string]$_.Name; Name=[string]$_.DisplayName; Scope='系统'; State=[string]$_.State; StartMode=[string]$_.StartMode; Command=[string]$_.PathName; PID=[int]$_.ProcessId} }) -Compress`
const startupScript = `$items=@();
foreach($scope in @('HKCU','HKLM')) {
 $run=$scope+':\Software\Microsoft\Windows\CurrentVersion\Run'; $backup=$scope+':\Software\Microsoft\Windows\CurrentVersion\SysboxDisabledRun';
 foreach($path in @($run,$backup)) { if(Test-Path -LiteralPath $path) { $key=Get-Item -LiteralPath $path; foreach($name in $key.GetValueNames()) { if($key.GetValueKind($name) -ne 'String' -and $key.GetValueKind($name) -ne 'ExpandString') { continue }; $state='已启用'; if($path -eq $backup){$state='已禁用'}; $items += [pscustomobject]@{ID=$path+'|'+$name; Name=$name; Scope=$scope; State=$state; StartMode='登录时启动'; Command=[string]$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); Path=$path; Domain=$name; Startup=$true} } } }
}
foreach($path in @([Environment]::GetFolderPath('Startup'),[Environment]::GetFolderPath('CommonStartup'))) { if($path -and (Test-Path -LiteralPath $path)) { foreach($file in Get-ChildItem -LiteralPath $path -File) { $items += [pscustomobject]@{ID=$file.FullName; Name=$file.Name; Scope='启动文件夹'; State='已启用'; StartMode='登录时启动'; Command=$file.FullName; Path=$file.FullName; Startup=$true} } } }
ConvertTo-Json -InputObject @($items) -Compress`

func (c *Client) list(ctx context.Context, startup bool) (Snapshot, error) {
	script := serviceScript
	if startup {
		script = startupScript
	}
	out, err := c.Runner.Run(ctx, ps(script))
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	err = json.Unmarshal([]byte(strings.TrimPrefix(out, "\ufeff")), &snapshot.Items)
	return snapshot, err
}

func Actions(item Item) []Action {
	if item.Startup {
		if item.Domain == "" {
			return nil
		}
		source := item.Path
		target := strings.Replace(source, `\Run`, `\SysboxDisabledRun`, 1)
		key, label := "d", "禁用"
		if strings.HasSuffix(source, `\SysboxDisabledRun`) {
			target = strings.TrimSuffix(source, `\SysboxDisabledRun`) + `\Run`
			key, label = "e", "启用"
		}
		// 校验源值未变化且目标没有同名值，再迁移原始字符串及注册表值类型，避免覆盖现有配置。
		script := `$source=` + quote(source) + `; $target=` + quote(target) + `; $name=` + quote(item.Domain) + `; $expected=` + quote(item.Command) + `; $key=Get-Item -LiteralPath $source; if($key.GetValueNames() -notcontains $name){throw '启动项已变化，请刷新'}; $value=$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); if($value -cne $expected){throw '启动项已变化，请刷新'}; if(Test-Path -LiteralPath $target){if((Get-Item -LiteralPath $target).GetValueNames() -contains $name){throw '目标已存在同名启动项，请先检查'}}; $kind=$key.GetValueKind($name); New-Item -Path $target -Force | Out-Null; New-ItemProperty -LiteralPath $target -Name $name -Value $value -PropertyType $kind | Out-Null; Remove-ItemProperty -LiteralPath $source -Name $name`
		return []Action{{Key: key, Label: label, Command: ps(script), Mutates: true, Note: "在 Run 与 SysboxDisabledRun 注册表项间移动；不结束当前运行程序。HKLM 项可能需要管理员权限"}}
	}
	name := quote(item.ID)
	actions := []Action{
		{Key: "s", Label: "启动", Command: ps("Start-Service -Name " + name), Mutates: true},
		{Key: "x", Label: "停止", Command: ps("Stop-Service -Name " + name), Mutates: true, Note: "不强制停止有依赖的服务"},
		{Key: "e", Label: "自动启动", Command: ps("Set-Service -Name " + name + " -StartupType Automatic"), Mutates: true, Note: "设置开机自动启动；不会立即启动"},
		{Key: "m", Label: "手动启动", Command: ps("Set-Service -Name " + name + " -StartupType Manual"), Mutates: true},
		{Key: "d", Label: "禁用", Command: ps("Set-Service -Name " + name + " -StartupType Disabled"), Mutates: true, Note: "禁用后无法启动；已经运行的服务不会立即停止"},
	}
	logs := `$name=` + quote(item.ID) + `; $display=` + quote(item.Name) + `; Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'; StartTime=(Get-Date).AddHours(-24)} -MaxEvents 1000 -ErrorAction SilentlyContinue | Where-Object {$_.Message -and ($_.Message.IndexOf($name,[StringComparison]::OrdinalIgnoreCase) -ge 0 -or $_.Message.IndexOf($display,[StringComparison]::OrdinalIgnoreCase) -ge 0)} | Select-Object -First 100 TimeCreated,Id,LevelDisplayName,Message | Format-List | Out-String -Width 180`
	return append(actions, Action{Key: "l", Label: "日志", Command: ps(logs), Note: "查询近 24 小时服务控制管理器的最近 1000 条事件，筛选所选服务"})
}
