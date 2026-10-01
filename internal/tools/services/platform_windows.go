package services

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"unicode/utf16"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

const psPrefix = `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false); `

func ps(script string) sysx.Cmd {
	return sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psPrefix+script)
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

// elevated 已是管理员时直接执行，否则弹出 UAC 授权后在提升的进程中执行；脚本以 Base64 传递，避免二次转义
func elevated(script string) sysx.Cmd {
	inner := psPrefix + "try { " + script + " } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }"
	units := utf16.Encode([]rune(inner))
	raw := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], u)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return ps(`$admin=(New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator); if($admin){ & powershell.exe -NoProfile -NonInteractive -EncodedCommand '` + encoded + `'; if($LASTEXITCODE){ throw "操作失败（退出码 $LASTEXITCODE）" } } else { $p=Start-Process powershell.exe -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','` + encoded + `'; if($p.ExitCode -ne 0){ throw "管理员操作失败（退出码 $($p.ExitCode)）" } }`)
}

const serviceScript = `ConvertTo-Json -InputObject @(Get-CimInstance Win32_Service | ForEach-Object { [pscustomobject]@{ID=[string]$_.Name; Name=[string]$_.DisplayName; State=[string]$_.State; Mode=[string]$_.StartMode; Command=[string]$_.PathName; PID=[int]$_.ProcessId} }) -Compress`

// 启动项与任务管理器一致：Run 注册表与启动文件夹的启用状态记录在 StartupApproved 中
const startupScript = `$items=@(); $base='\Software\Microsoft\Windows\CurrentVersion'
function Approved($key,$name){ try { $v=(Get-ItemProperty -LiteralPath $key -Name $name -ErrorAction Stop).$name; if($v -is [byte[]] -and $v.Length -gt 0){ return (($v[0] -band 1) -eq 0) } } catch {}; return $true }
$sources=@(@{Hive='HKCU';Run="HKCU:$base\Run";Approval="HKCU:$base\Explorer\StartupApproved\Run"}, @{Hive='HKLM';Run="HKLM:$base\Run";Approval="HKLM:$base\Explorer\StartupApproved\Run"}, @{Hive='HKLM';Run='HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run';Approval="HKLM:$base\Explorer\StartupApproved\Run32"})
foreach($s in $sources){ if(Test-Path -LiteralPath $s.Run){ $key=Get-Item -LiteralPath $s.Run; foreach($name in $key.GetValueNames()){ if($name -eq ''){ continue }; $kind=$key.GetValueKind($name); if($kind -ne 'String' -and $kind -ne 'ExpandString'){ continue }; $items+=[pscustomobject]@{ID=$s.Run+'|'+$name; Name=$name; Kind='run'; Scope=$s.Hive; Command=[string]$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); Path=$s.Run; Domain=$name; Approval=$s.Approval; Disabled=(-not (Approved $s.Approval $name))} } } }
foreach($hive in @('HKCU','HKLM')){ $legacy=$hive+":$base\SysboxDisabledRun"; if(Test-Path -LiteralPath $legacy){ $key=Get-Item -LiteralPath $legacy; foreach($name in $key.GetValueNames()){ $items+=[pscustomobject]@{ID=$legacy+'|'+$name; Name=$name; Kind='legacy'; Scope=$hive; Command=[string]$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); Path=$legacy; Domain=$name; Disabled=$true} } } }
foreach($f in @(@{Path=[Environment]::GetFolderPath('Startup');Hive='HKCU'}, @{Path=[Environment]::GetFolderPath('CommonStartup');Hive='HKLM'})){ $approval=$f.Hive+":$base\Explorer\StartupApproved\StartupFolder"; if($f.Path -and (Test-Path -LiteralPath $f.Path)){ foreach($file in Get-ChildItem -LiteralPath $f.Path -File){ if($file.Name -eq 'desktop.ini'){ continue }; $items+=[pscustomobject]@{ID=$file.FullName; Name=$file.Name; Kind='folder'; Scope=$f.Hive; Command=$file.FullName; Path=$file.FullName; Domain=$file.Name; Approval=$approval; Disabled=(-not (Approved $approval $file.Name))} } } }
try { foreach($t in @(Get-ScheduledTask -ErrorAction Stop | Where-Object { $_.TaskPath -notlike '\Microsoft\*' -and @($_.Triggers | Where-Object { $_.CimClass.CimClassName -in @('MSFT_TaskLogonTrigger','MSFT_TaskBootTrigger') }).Count -gt 0 })){ $cmd=@($t.Actions | ForEach-Object { (([string]$_.Execute)+' '+([string]$_.Arguments)).Trim() }) -join '; '; $items+=[pscustomobject]@{ID=$t.TaskPath+$t.TaskName; Name=$t.TaskName; Kind='task'; Scope='HKLM'; Command=$cmd; Path=$t.TaskPath; Domain=$t.TaskName; Disabled=([string]$t.State -eq 'Disabled'); Running=([string]$t.State -eq 'Running')} } } catch {}
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
	if err := json.Unmarshal([]byte(strings.TrimPrefix(out, "\ufeff")), &snapshot.Items); err != nil {
		return snapshot, err
	}
	for i := range snapshot.Items {
		if startup {
			decorateStartup(&snapshot.Items[i])
		} else {
			decorateService(&snapshot.Items[i])
		}
	}
	return snapshot, nil
}

var startModes = map[string]string{"Auto": "自动", "Manual": "手动", "Disabled": "已禁用", "Boot": "引导", "System": "系统"}

func decorateService(item *Item) {
	item.Kind, item.Scope, item.Admin = "service", "系统", true
	item.Source = SourceThirdParty
	if strings.Contains(strings.ToLower(item.Command), `\windows\`) {
		item.Source = SourceSystem
	}
	item.Running = item.State == "Running"
	item.Disabled = item.Mode == "Disabled"
	// 设为自动启动却没有运行，通常是启动失败
	item.Failed = item.Mode == "Auto" && item.State == "Stopped"
	item.StartMode = startModes[item.Mode]
	if item.StartMode == "" {
		item.StartMode = item.Mode
	}
	switch {
	case item.Running:
		item.State = "运行中"
	case item.Failed:
		item.State = "应运行未运行"
	case item.State == "Stopped":
		item.State = "已停止"
	}
}

func decorateStartup(item *Item) {
	item.Startup, item.Source = true, SourceThirdParty
	item.Admin = item.Scope == "HKLM"
	scopes := map[string]string{"HKCU": "当前用户", "HKLM": "所有用户"}
	switch item.Kind {
	case "task":
		item.Scope, item.StartMode = "计划任务", "开机或登录时"
	case "folder":
		item.StartMode = "启动文件夹"
	case "legacy":
		item.StartMode = "旧版 sysbox 禁用"
	default:
		item.StartMode = "登录时启动"
	}
	if label, ok := scopes[item.Scope]; ok {
		item.Scope = label
	}
	switch {
	case item.Running:
		item.State = "运行中"
	case item.Disabled:
		item.State = "已禁用"
	default:
		item.State = "已启用"
	}
}

func Actions(item Item) []Action {
	if item.Startup {
		return startupActions(item)
	}
	name := quote(item.ID)
	var actions []Action
	if item.Running {
		actions = append(actions,
			Action{Key: "s", Label: "停止", Preview: "Stop-Service -Name " + name, Command: elevated("Stop-Service -Name " + name), Mutates: true, Note: "不强制停止有依赖的服务"},
			Action{Key: "R", Label: "重启", Preview: "Restart-Service -Name " + name, Command: elevated("Restart-Service -Name " + name), Mutates: true})
	} else if !item.Disabled {
		actions = append(actions, Action{Key: "s", Label: "启动", Preview: "Start-Service -Name " + name, Command: elevated("Start-Service -Name " + name), Mutates: true})
	}
	for _, m := range []struct{ key, label, value, mode, note string }{
		{"e", "自动启动", "Automatic", "Auto", "开机时自动启动；不会立即启动"},
		{"m", "手动启动", "Manual", "Manual", "仅在需要时由系统或程序启动"},
		{"d", "禁用", "Disabled", "Disabled", "禁用后无法启动；已经运行的服务不会立即停止"},
	} {
		if item.Mode == m.mode {
			continue
		}
		script := "Set-Service -Name " + name + " -StartupType " + m.value
		actions = append(actions, Action{Key: m.key, Label: m.label, Preview: script, Command: elevated(script), Mutates: true, Note: m.note})
	}
	for i := range actions {
		actions[i].Note = strings.TrimPrefix(actions[i].Note+"；需要管理员授权", "；")
	}
	logs := `$name=` + quote(item.ID) + `; $display=` + quote(item.Name) + `; Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'; StartTime=(Get-Date).AddHours(-24)} -MaxEvents 1000 -ErrorAction SilentlyContinue | Where-Object {$_.Message -and ($_.Message.IndexOf($name,[StringComparison]::OrdinalIgnoreCase) -ge 0 -or $_.Message.IndexOf($display,[StringComparison]::OrdinalIgnoreCase) -ge 0)} | Select-Object -First 100 TimeCreated,Id,LevelDisplayName,Message | Format-List | Out-String -Width 180`
	return append(actions, Action{Key: "l", Label: "日志", Command: ps(logs), Note: "查询近 24 小时服务控制管理器的事件"})
}

func startupActions(item Item) []Action {
	var actions []Action
	wrap := func(key, label, script, note string) {
		command := ps(script)
		if item.Admin {
			command = elevated(script)
			note += "；需要管理员授权"
		}
		actions = append(actions, Action{Key: key, Label: label, Preview: script, Command: command, Mutates: true, Note: note})
	}
	switch item.Kind {
	case "run", "folder":
		// 与任务管理器相同：首字节 02 表示启用，03 表示禁用，不移动原始启动项
		value, label, note := "3", "禁用", "下次登录时不再启动；不结束当前运行的程序，任务管理器中同步显示为已禁用"
		if item.Disabled {
			value, label, note = "2", "启用", "下次登录时启动"
		}
		script := `$key=` + quote(item.Approval) + `; if(-not (Test-Path -LiteralPath $key)){ New-Item -Path $key -Force | Out-Null }; New-ItemProperty -LiteralPath $key -Name ` + quote(item.Domain) + ` -PropertyType Binary -Value ([byte[]](` + value + `,0,0,0,0,0,0,0,0,0,0,0)) -Force | Out-Null`
		wrap("e", label, script, note)
	case "legacy":
		// 旧版本把禁用项移动到了 SysboxDisabledRun，这里移回 Run，之后统一用 StartupApproved 管理
		source := item.Path
		target := strings.TrimSuffix(source, `\SysboxDisabledRun`) + `\Run`
		script := `$source=` + quote(source) + `; $target=` + quote(target) + `; $name=` + quote(item.Domain) + `; $expected=` + quote(item.Command) + `; $key=Get-Item -LiteralPath $source; if($key.GetValueNames() -notcontains $name){throw '启动项已变化，请刷新'}; $value=$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); if($value -cne $expected){throw '启动项已变化，请刷新'}; if(Test-Path -LiteralPath $target){if((Get-Item -LiteralPath $target).GetValueNames() -contains $name){throw '目标已存在同名启动项，请先检查'}}; $kind=$key.GetValueKind($name); New-Item -Path $target -Force | Out-Null; New-ItemProperty -LiteralPath $target -Name $name -Value $value -PropertyType $kind | Out-Null; Remove-ItemProperty -LiteralPath $source -Name $name`
		wrap("e", "恢复", script, "移回 Run 注册表项，恢复登录时启动")
	case "task":
		verb, label, note := "Disable-ScheduledTask", "禁用", "禁用计划任务，开机或登录时不再运行"
		if item.Disabled {
			verb, label, note = "Enable-ScheduledTask", "启用", "启用计划任务"
		}
		item.Admin = true
		wrap("e", label, verb+" -TaskPath "+quote(item.Path)+" -TaskName "+quote(item.Domain)+" | Out-Null", note)
	}
	if item.Kind == "folder" {
		actions = append(actions, Action{Key: "o", Label: "显示位置", Command: sysx.C("explorer.exe", "/select,"+item.Path)})
	}
	return actions
}
