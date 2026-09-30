//go:build windows

package processes

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsSample struct {
	started string
	ticks   uint64
	at      time.Time
	user    string
}

type windowsProvider struct {
	mu      sync.Mutex
	samples map[int]windowsSample
}

func NewProvider() Provider { return &windowsProvider{samples: make(map[int]windowsSample)} }

// 一次批量查询进程和端点，避免逐进程启动 PowerShell；CPU 使用累计时间差计算。
const windowsSnapshotScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$items = @(Get-CimInstance Win32_Process | ForEach-Object {
    $started = ''
    if ($_.CreationDate) { $started = $_.CreationDate.ToUniversalTime().ToString('o') }
    [pscustomobject]@{ PID=[int]$_.ProcessId; PPID=[int]$_.ParentProcessId; Name=[string]$_.Name; Path=[string]$_.ExecutablePath; Command=[string]$_.CommandLine; Started=$started; Memory=[uint64]$_.WorkingSetSize; Ticks=([uint64]$_.KernelModeTime + [uint64]$_.UserModeTime) }
})
$ports = @()
$warnings = @()
try {
    $ports += @(Get-NetTCPConnection -ErrorAction Stop | ForEach-Object {
        [pscustomobject]@{ PID=[int]$_.OwningProcess; Protocol='TCP'; Address=[string]$_.LocalAddress; Number=[int]$_.LocalPort; RemoteAddress=[string]$_.RemoteAddress; RemotePort=[int]$_.RemotePort; State=[string]$_.State }
    })
} catch { $warnings += '无法读取 TCP 端口：' + $_.Exception.Message }
try {
    $ports += @(Get-NetUDPEndpoint -ErrorAction Stop | ForEach-Object {
        [pscustomobject]@{ PID=[int]$_.OwningProcess; Protocol='UDP'; Address=[string]$_.LocalAddress; Number=[int]$_.LocalPort; RemoteAddress=''; RemotePort=0; State='BOUND' }
    })
} catch { $warnings += '无法读取 UDP 端口：' + $_.Exception.Message }
[pscustomobject]@{ Processes=$items; Ports=$ports; Warning=($warnings -join '; ') } | ConvertTo-Json -Depth 5 -Compress`

type windowsRawProcess struct {
	PID, PPID                    int
	Name, Path, Command, Started string
	Memory, Ticks                uint64
}

type windowsRawPort struct {
	PID, Number, RemotePort                 int
	Protocol, Address, RemoteAddress, State string
}

type windowsRawSnapshot struct {
	Processes []windowsRawProcess
	Ports     []windowsRawPort
	Warning   string
}

func runWindowsQuery(ctx context.Context, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("查询进程超时或已取消：%w", ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("Windows 进程查询失败：%w：%s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (p *windowsProvider) Snapshot(ctx context.Context) (Snapshot, error) {
	out, err := runWindowsQuery(ctx, windowsSnapshotScript)
	if err != nil {
		return Snapshot{}, err
	}
	var raw windowsRawSnapshot
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(out), "\ufeff")), &raw); err != nil {
		return Snapshot{}, fmt.Errorf("解析 Windows 进程信息失败：%w", err)
	}
	ports := make(map[int][]Port)
	for _, port := range raw.Ports {
		state := strings.ToUpper(port.State)
		if state == "LISTEN" || state == "LISTENING" {
			state = "LISTEN"
		}
		remote := ""
		if port.RemoteAddress != "" && port.RemotePort != 0 {
			remote = net.JoinHostPort(port.RemoteAddress, strconv.Itoa(port.RemotePort))
		}
		ports[port.PID] = append(ports[port.PID], Port{Protocol: port.Protocol, Local: net.JoinHostPort(port.Address, strconv.Itoa(port.Number)), Remote: remote, State: state, Number: port.Number})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	next := make(map[int]windowsSample, len(raw.Processes))
	result := Snapshot{Warning: raw.Warning, Processes: make([]Process, 0, len(raw.Processes))}
	if len(p.samples) == 0 {
		if result.Warning != "" {
			result.Warning += "；"
		}
		result.Warning += "首次采样的 CPU 显示为 0，下一次自动刷新后显示实际占用"
	}
	for _, item := range raw.Processes {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		previous, exists := p.samples[item.PID]
		cpu := windowsCPUPercent(previous, item, now)
		user := previous.user
		if !exists || previous.started != item.Started {
			user = windowsProcessUser(item.PID)
		}
		next[item.PID] = windowsSample{started: item.Started, ticks: item.Ticks, at: now, user: user}
		result.Processes = append(result.Processes, Process{PID: item.PID, PPID: item.PPID, Name: item.Name, Path: item.Path, Command: item.Command, Started: item.Started, CPU: cpu, Memory: item.Memory, User: user, Ports: ports[item.PID]})
	}
	p.samples = next
	return result, nil
}

func windowsCPUPercent(previous windowsSample, current windowsRawProcess, now time.Time) float64 {
	elapsed := now.Sub(previous.at).Seconds()
	if previous.started == "" || previous.started != current.Started || elapsed <= 0 || current.Ticks < previous.ticks {
		return 0
	}
	// Windows 的累计 CPU 时间以 100 纳秒为单位，100% 表示一个逻辑核心。
	return float64(current.Ticks-previous.ticks) / 1e7 / elapsed * 100
}

func windowsProcessUser(pid int) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "未知/无权限"
	}
	defer windows.CloseHandle(handle)
	var token windows.Token
	if windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token) != nil {
		return "未知/无权限"
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "未知/无权限"
	}
	account, domain, _, err := user.User.Sid.LookupAccount("")
	if err != nil {
		return "未知/无权限"
	}
	if domain != "" {
		return domain + `\` + account
	}
	return account
}

func (p *windowsProvider) Details(ctx context.Context, process Process) (Details, error) {
	if err := ctx.Err(); err != nil {
		return Details{}, err
	}
	result := Details{}
	if process.PID == os.Getpid() {
		result.Environment = make(map[string]string)
		for _, entry := range os.Environ() {
			key, value, ok := strings.Cut(entry, "=")
			if ok && key != "" {
				result.Environment[key] = value
			}
		}
	} else {
		handle, err := verifiedWindowsProcess(process, windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ)
		if err != nil {
			return Details{}, err
		}
		environment, err := readWindowsEnvironment(ctx, handle)
		windows.CloseHandle(handle)
		if err != nil {
			result.Warning = "读取进程环境失败：" + err.Error()
		} else {
			result.Environment = environment
		}
	}
	if process.PPID <= 0 {
		return result, nil
	}
	parent, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(process.PPID))
	if err == nil {
		defer windows.CloseHandle(parent)
		var created, exited, kernel, user windows.Filetime
		err = windows.GetProcessTimes(parent, &created, &exited, &kernel, &user)
		if err == nil {
			childStarted, parseErr := time.Parse(time.RFC3339Nano, process.Started)
			if parseErr != nil || time.Unix(0, created.Nanoseconds()).Truncate(time.Microsecond).After(childStarted.Truncate(time.Microsecond)) {
				err = errors.New("父进程 PID 已复用或启动时间无法核对")
			}
		}
		if err == nil {
			result.ParentEnvironment, err = readWindowsEnvironment(ctx, parent)
		}
	}
	if err != nil {
		if result.Warning != "" {
			result.Warning += "；"
		}
		result.Warning += "读取父进程环境失败：" + err.Error()
	}
	return result, nil
}

// PEB 读取依赖 Windows 进程内部布局，仅支持相同指针宽度；访问受保护进程时明确返回权限错误。
func readWindowsEnvironment(ctx context.Context, handle windows.Handle) (map[string]string, error) {
	var targetWow, ownWow bool
	if err := windows.IsWow64Process(handle, &targetWow); err != nil {
		return nil, err
	}
	if err := windows.IsWow64Process(windows.CurrentProcess(), &ownWow); err != nil {
		return nil, err
	}
	if targetWow != ownWow {
		return nil, errors.New("暂不支持跨 32/64 位读取进程环境")
	}
	var basic windows.PROCESS_BASIC_INFORMATION
	var returned uint32
	if err := windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), &returned); err != nil {
		return nil, fmt.Errorf("查询 PEB 失败（进程可能已退出或权限不足）：%w", err)
	}
	if basic.PebBaseAddress == nil {
		return nil, errors.New("进程没有可读取的 PEB")
	}
	parameters, err := readWindowsPointer(handle, uintptr(unsafe.Pointer(basic.PebBaseAddress))+unsafe.Offsetof(windows.PEB{}.ProcessParameters))
	if err != nil {
		return nil, err
	}
	environment, err := readWindowsPointer(handle, parameters+unsafe.Offsetof(windows.RTL_USER_PROCESS_PARAMETERS{}.Environment))
	if err != nil {
		return nil, err
	}
	if environment == 0 {
		return nil, errors.New("进程环境地址不可用")
	}
	// 逐内存区域读取，避免跨入不可访问页；最大 1 MiB，防止变化中的地址产生无界读取。
	const maxBytes = 1024 * 1024
	data := make([]byte, 0, 4096)
	for len(data) < maxBytes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		address := environment + uintptr(len(data))
		var region windows.MemoryBasicInformation
		if err := windows.VirtualQueryEx(handle, address, &region, unsafe.Sizeof(region)); err != nil {
			return nil, fmt.Errorf("读取环境内存区域失败：%w", err)
		}
		if region.State != windows.MEM_COMMIT || region.Protect&(windows.PAGE_NOACCESS|windows.PAGE_GUARD) != 0 {
			return nil, errors.New("环境内存不可访问（权限不足或进程正在退出）")
		}
		end := region.BaseAddress + region.RegionSize
		if end <= address {
			return nil, errors.New("环境内存区域地址无效")
		}
		size := min(int(end-address), 4096, maxBytes-len(data))
		chunk := make([]byte, size)
		var read uintptr
		if err := windows.ReadProcessMemory(handle, address, &chunk[0], uintptr(size), &read); err != nil {
			return nil, fmt.Errorf("读取环境内存失败：%w", err)
		}
		if read == 0 {
			return nil, errors.New("环境内存未返回数据")
		}
		data = append(data, chunk[:read]...)
		if environmentTerminator(data) >= 0 {
			return parseWindowsEnvironment(data)
		}
	}
	return nil, errors.New("进程环境超过 1 MiB 或缺少结束标记")
}

func readWindowsPointer(handle windows.Handle, address uintptr) (uintptr, error) {
	if address == 0 {
		return 0, errors.New("进程参数地址不可用")
	}
	data := make([]byte, unsafe.Sizeof(uintptr(0)))
	var read uintptr
	if err := windows.ReadProcessMemory(handle, address, &data[0], uintptr(len(data)), &read); err != nil {
		return 0, fmt.Errorf("读取进程参数失败：%w", err)
	}
	if read != uintptr(len(data)) {
		return 0, errors.New("进程参数读取不完整")
	}
	if len(data) == 4 {
		return uintptr(binary.LittleEndian.Uint32(data)), nil
	}
	return uintptr(binary.LittleEndian.Uint64(data)), nil
}

func environmentTerminator(data []byte) int {
	for i := 0; i+3 < len(data); i += 2 {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 0 {
			return i
		}
	}
	return -1
}

func parseWindowsEnvironment(data []byte) (map[string]string, error) {
	end := environmentTerminator(data)
	if end < 0 {
		return nil, errors.New("环境变量缺少 UTF-16 双空字符结束标记")
	}
	units := make([]uint16, end/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	result := make(map[string]string)
	for _, entry := range strings.Split(string(utf16.Decode(units)), "\x00") {
		if entry == "" {
			continue
		}
		// Windows 的 =C:=... 项表示每个驱动器的工作目录，保留其完整变量名。
		index := strings.Index(entry[1:], "=") + 1
		if index <= 0 {
			return nil, errors.New("环境变量条目格式无效")
		}
		result[entry[:index]] = entry[index+1:]
	}
	return result, nil
}

func verifiedWindowsProcess(process Process, access uint32) (windows.Handle, error) {
	if process.PID <= 0 {
		return 0, errors.New("进程 PID 无效")
	}
	if process.Started == "" {
		return 0, errors.New("缺少进程启动时间，无法核对操作目标；请刷新后重试")
	}
	expected, err := time.Parse(time.RFC3339Nano, process.Started)
	if err != nil {
		return 0, fmt.Errorf("无法核对进程启动时间：%w", err)
	}
	handle, err := windows.OpenProcess(access|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.PID))
	if err != nil {
		return 0, fmt.Errorf("无法打开进程，可能已退出或权限不足：%w", err)
	}
	valid := false
	defer func() {
		if !valid {
			windows.CloseHandle(handle)
		}
	}()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, fmt.Errorf("核对进程身份失败：%w", err)
	}
	// CIM 的 CreationDate 精度为微秒，而内核时间精度为 100 纳秒。
	if !time.Unix(0, created.Nanoseconds()).UTC().Truncate(time.Microsecond).Equal(expected.UTC().Truncate(time.Microsecond)) {
		return 0, errors.New("PID 已被其他进程复用，请刷新后重试")
	}
	if process.Path != "" {
		buf := make([]uint16, 32768)
		size := uint32(len(buf))
		if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
			return 0, fmt.Errorf("核对程序路径失败：%w", err)
		}
		if !strings.EqualFold(windows.UTF16ToString(buf[:size]), process.Path) {
			return 0, errors.New("进程路径已变化，请刷新后重试")
		}
	}
	valid = true
	return handle, nil
}

var (
	processUser32        = windows.NewLazySystemDLL("user32.dll")
	processEnumWindows   = processUser32.NewProc("EnumWindows")
	processWindowPID     = processUser32.NewProc("GetWindowThreadProcessId")
	processPostMessage   = processUser32.NewProc("PostMessageW")
	processCloseMutex    sync.Mutex
	processCloseRequest  windowsCloseRequest
	processCloseCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		request := &processCloseRequest
		var pid uint32
		processWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == request.pid {
			ok, _, _ := processPostMessage.Call(hwnd, 0x0010, 0, 0)
			if ok != 0 {
				request.closed++
			}
		}
		return 1
	})
)

type windowsCloseRequest struct {
	pid    uint32
	closed int
}

func (p *windowsProvider) Terminate(ctx context.Context, process Process, force bool) error {
	if process.PID <= 4 || process.PID == os.Getpid() {
		return errors.New("不能操作系统关键进程或 sysbox 自身")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if force {
		access |= windows.PROCESS_TERMINATE
	}
	handle, err := verifiedWindowsProcess(process, access)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if force {
		if err := windows.TerminateProcess(handle, 1); err != nil {
			return fmt.Errorf("强制结束进程失败：%w", err)
		}
		return nil
	}
	// 普通结束发送 WM_CLOSE，让有窗口的应用处理退出；控制台进程没有通用的温和结束接口。
	processCloseMutex.Lock()
	defer processCloseMutex.Unlock()
	processCloseRequest = windowsCloseRequest{pid: uint32(process.PID)}
	ok, _, enumErr := processEnumWindows.Call(processCloseCallback, 0)
	if ok == 0 {
		return fmt.Errorf("请求关闭进程窗口失败：%w", enumErr)
	}
	if processCloseRequest.closed == 0 {
		return errors.New("该进程没有可关闭的窗口；Windows 不支持通用的温和结束，请使用程序自身的退出操作或选择强制结束")
	}
	return nil
}
