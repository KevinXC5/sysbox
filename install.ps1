# sysbox 安装脚本：
#   irm https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.ps1 | iex
#
# 环境变量：
#   SYSBOX_VERSION      安装指定版本，如 v0.1.12；默认最新版本
#   SYSBOX_INSTALL_DIR  安装目录，默认 $env:LOCALAPPDATA\Programs\sysbox
# 已安装后可直接用 sysbox update 升级，界面里也会提示新版本。
# 兼容 Windows PowerShell 5.1。
$ErrorActionPreference = 'Stop'
# 通过 iex 执行时默认在函数作用域，显式提升后 die 里的 exit 才能结束整个脚本
Set-StrictMode -Off

$Repo = 'KevinXC5/sysbox'
if ($env:SYSBOX_INSTALL_DIR) {
    $InstallDir = $env:SYSBOX_INSTALL_DIR
} else {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\sysbox'
}
if ($env:SYSBOX_VERSION) {
    $Version = $env:SYSBOX_VERSION
} else {
    $Version = 'latest'
}

function Write-Info([string]$Message) {
    Write-Host ("◆ " + $Message) -ForegroundColor Magenta
}
function Write-Ok([string]$Message) {
    Write-Host ("✓ " + $Message) -ForegroundColor Green
}
function Write-Die([string]$Message) {
    Write-Host ("✗ " + $Message) -ForegroundColor Red
    exit 1
}

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { '' }
}
if (-not $Arch) {
    Write-Die ("不支持的架构：" + $env:PROCESSOR_ARCHITECTURE)
}
$Asset = "sysbox-windows-$Arch.exe"

if ($Version -eq 'latest') {
    $Base = "https://github.com/$Repo/releases/latest/download"
} else {
    $Base = "https://github.com/$Repo/releases/download/$Version"
}

$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("sysbox-install-" + [guid]::NewGuid().ToString('n'))
New-Item -ItemType Directory -Path $Tmp | Out-Null
try {
    $BinPath = Join-Path $Tmp $Asset
    $SumPath = Join-Path $Tmp 'checksums.txt'

    Write-Info "下载 $Asset（$Version）"
    try {
        Invoke-WebRequest -Uri "$Base/$Asset" -OutFile $BinPath -UseBasicParsing
        Invoke-WebRequest -Uri "$Base/checksums.txt" -OutFile $SumPath -UseBasicParsing
    } catch {
        Write-Die "下载失败：$Base/$Asset"
    }

    $Want = ''
    foreach ($Line in Get-Content -Path $SumPath) {
        $Parts = $Line -split '\s+'
        if ($Parts.Length -ge 2 -and $Parts[1] -eq $Asset) {
            $Want = $Parts[0].ToLowerInvariant()
            break
        }
    }
    $Got = (Get-FileHash -Algorithm SHA256 -Path $BinPath).Hash.ToLowerInvariant()
    if (-not $Want -or $Want -ne $Got) {
        Write-Die 'SHA-256 校验失败，未安装'
    }
    Write-Ok '校验通过'

    if (-not (Test-Path -LiteralPath $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir | Out-Null
    }
    $Dest = Join-Path $InstallDir 'sysbox.exe'
    # 正在运行的 exe 不能直接覆盖，先改名为 .old，下次启动时由程序自行清理
    if (Test-Path -LiteralPath $Dest) {
        $Old = "$Dest.old"
        if (Test-Path -LiteralPath $Old) {
            Remove-Item -LiteralPath $Old -Force -ErrorAction SilentlyContinue
        }
        try {
            Rename-Item -LiteralPath $Dest -NewName 'sysbox.exe.old' -ErrorAction Stop
        } catch {
            Write-Die '无法移开正在运行的 sysbox.exe，请先退出后重试'
        }
    }
    Move-Item -LiteralPath $BinPath -Destination $Dest -Force
    $VerLine = & $Dest version
    Write-Ok "已安装 $VerLine 到 $Dest"

    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $UserPath) { $UserPath = '' }
    $Parts = @($UserPath -split ';' | Where-Object { $_ -ne '' })
    $Exists = $false
    foreach ($Part in $Parts) {
        if ($Part.TrimEnd('\') -eq $InstallDir.TrimEnd('\')) {
            $Exists = $true
            break
        }
    }
    if (-not $Exists) {
        if ($UserPath) {
            $NewPath = "$InstallDir;$UserPath"
        } else {
            $NewPath = $InstallDir
        }
        [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
        Write-Ok "已把 $InstallDir 加入用户 PATH"
    }
    # 用户 PATH 只对新开的终端生效；irm | iex 在当前会话执行，同步更新当前会话 PATH 后可直接运行 sysbox
    $InSession = $false
    foreach ($Part in @($env:Path -split ';')) {
        if ($Part.TrimEnd('\') -eq $InstallDir.TrimEnd('\')) {
            $InSession = $true
            break
        }
    }
    if (-not $InSession) {
        $env:Path = "$InstallDir;$env:Path"
    }
    Write-Host ''
    Write-Host '现在可以运行 sysbox；若提示找不到命令，请关闭并重新打开终端'
} finally {
    if (Test-Path -LiteralPath $Tmp) {
        Remove-Item -LiteralPath $Tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
