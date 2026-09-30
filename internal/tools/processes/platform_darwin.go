//go:build darwin

package processes

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type darwinCPUSample struct {
	Started, Path string
	Total         time.Duration
}

type darwinProvider struct {
	mu        sync.Mutex
	previous  map[int]darwinCPUSample
	sampledAt time.Time
}

func NewProvider() Provider { return &darwinProvider{} }

func darwinCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// 日期解析和数字解析固定使用 C locale，保留调用方的其他环境变量。
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.Output()
}

func (provider *darwinProvider) Snapshot(ctx context.Context) (Snapshot, error) {
	// 串行采样避免并发刷新打乱 CPU 基线；其他详情操作无需等待此锁。
	provider.mu.Lock()
	defer provider.mu.Unlock()
	data, err := darwinCommand(ctx, "/bin/ps", "-ww", "-axo", "pid=,ppid=,time=,rss=,user=,lstart=,args=")
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取进程列表失败：%w", err)
	}
	sampledAt := time.Now()
	processes, totals := parseDarwinProcessesAndTimes(string(data))
	result := Snapshot{Processes: processes}
	paths, pathErr := darwinCommand(ctx, "/bin/ps", "-ww", "-axo", "pid=,comm=")
	pathMap := parseDarwinPaths(string(paths))
	if pathErr != nil {
		result.Warning = "部分进程的程序路径无法读取"
	}
	ports, portErr := darwinCommand(ctx, "/usr/sbin/lsof", "-nP", "-iTCP", "-iUDP", "-FpfPnT")
	portMap := parseDarwinPorts(string(ports))
	if portErr != nil && !(isExitOne(portErr) && len(ports) == 0) {
		result.Warning = joinDarwinWarnings(result.Warning, "部分端口无法读取："+portErr.Error())
	}
	for i := range result.Processes {
		p := &result.Processes[i]
		p.Path = pathMap[p.PID]
		p.Name = filepath.Base(p.Path)
		if p.Path == "" {
			p.Name = strings.SplitN(p.Command, " ", 2)[0]
		}
		p.Ports = portMap[p.PID]
	}
	if provider.sampleCPU(result.Processes, totals, sampledAt) {
		result.Warning = joinDarwinWarnings(result.Warning, "首次刷新正在采样 CPU，下次刷新显示实时占用")
	}
	// lsof 对其他用户的进程可能只提供部分结果，不能把空结果当作没有端口。
	if os.Geteuid() != 0 {
		result.Warning = joinDarwinWarnings(result.Warning, "其他用户的进程端口可能因权限不足而不可见")
	}
	return result, nil
}

func isExitOne(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// 只消费前十列，完整命令中的空格仍保留。
func takeDarwinFields(line string, count int) ([]string, string) {
	fields := make([]string, 0, count)
	for len(fields) < count {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			return fields, ""
		}
		end := strings.IndexAny(line, " \t")
		if end < 0 {
			return append(fields, line), ""
		}
		fields = append(fields, line[:end])
		line = line[end:]
	}
	return fields, strings.TrimSpace(line)
}

func parseDarwinProcesses(data string) []Process {
	processes, _ := parseDarwinProcessesAndTimes(data)
	return processes
}

func parseDarwinProcessesAndTimes(data string) ([]Process, map[int]time.Duration) {
	var result []Process
	totals := make(map[int]time.Duration)
	for _, line := range strings.Split(data, "\n") {
		f, command := takeDarwinFields(line, 10)
		if len(f) != 10 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		totals[pid] = sysx.ParseClock(f[2])
		rss, _ := strconv.ParseUint(f[3], 10, 64)
		result = append(result, Process{PID: pid, PPID: ppid, Memory: rss * 1024, User: f[4], Started: strings.Join(f[5:10], " "), Command: command})
	}
	return result, totals
}

func (provider *darwinProvider) sampleCPU(processes []Process, totals map[int]time.Duration, now time.Time) bool {
	first := provider.sampledAt.IsZero()
	elapsed := now.Sub(provider.sampledAt).Seconds()
	next := make(map[int]darwinCPUSample, len(processes))
	for i := range processes {
		p := &processes[i]
		current := darwinCPUSample{Started: p.Started, Path: p.Path, Total: totals[p.PID]}
		previous, ok := provider.previous[p.PID]
		// 累计 CPU 时间差除以壁钟时间，多核进程允许超过 100%。
		if ok && !first && elapsed > 0 && previous.Started == current.Started && previous.Path == current.Path && current.Total >= previous.Total {
			p.CPU = (current.Total - previous.Total).Seconds() / elapsed * 100
		}
		next[p.PID] = current
	}
	provider.previous, provider.sampledAt = next, now
	return first
}

func parseDarwinPaths(data string) map[int]string {
	result := make(map[int]string)
	for _, line := range strings.Split(data, "\n") {
		f, path := takeDarwinFields(line, 1)
		if len(f) != 1 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err == nil {
			result[pid] = path
		}
	}
	return result
}

func parseDarwinPorts(data string) map[int][]Port {
	result := make(map[int][]Port)
	pid := 0
	port := Port{}
	flush := func() {
		if pid == 0 || port.Local == "" || port.Protocol == "" {
			return
		}
		if port.Protocol == "UDP" {
			port.State = "BOUND"
		}
		for _, existing := range result[pid] {
			if existing == port {
				return
			}
		}
		result[pid] = append(result[pid], port)
	}
	for _, line := range strings.Split(data, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			flush()
			pid, _ = strconv.Atoi(line[1:])
			port = Port{}
		case 'f':
			flush()
			port = Port{}
		case 'P':
			port.Protocol = line[1:]
		case 'n':
			addresses := strings.SplitN(line[1:], "->", 2)
			port.Local = addresses[0]
			if len(addresses) == 2 {
				port.Remote = addresses[1]
			}
			if colon := strings.LastIndex(port.Local, ":"); colon >= 0 {
				port.Number, _ = strconv.Atoi(port.Local[colon+1:])
			}
		case 'T':
			if strings.HasPrefix(line, "TST=") {
				port.State = strings.TrimPrefix(line, "TST=")
			}
		}
	}
	flush()
	return result
}

func joinDarwinWarnings(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "；" + b
}

func darwinCurrentProcess(ctx context.Context, pid int) (Process, error) {
	data, err := darwinCommand(ctx, "/bin/ps", "-ww", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,time=,rss=,user=,lstart=,args=")
	if err != nil {
		return Process{}, fmt.Errorf("目标进程已退出或无法读取：%w", err)
	}
	processes := parseDarwinProcesses(string(data))
	if len(processes) != 1 {
		return Process{}, errors.New("目标进程已退出")
	}
	paths, err := darwinCommand(ctx, "/bin/ps", "-ww", "-p", strconv.Itoa(pid), "-o", "pid=,comm=")
	if err != nil {
		return Process{}, fmt.Errorf("无法核对目标程序路径：%w", err)
	}
	p := processes[0]
	p.Path = parseDarwinPaths(string(paths))[pid]
	return p, nil
}

func validateDarwinIdentity(expected, current Process) error {
	if expected.PID <= 1 || expected.PID == os.Getpid() {
		return errors.New("不能操作系统初始进程或 sysbox 自身")
	}
	if expected.PID != current.PID || expected.Started == "" || expected.Started != current.Started {
		return errors.New("进程身份已变化，请刷新列表后重试")
	}
	if expected.Path == "" || current.Path == "" || expected.Path != current.Path {
		return errors.New("无法确认目标程序路径，请刷新列表后重试")
	}
	return nil
}

func (*darwinProvider) Terminate(ctx context.Context, expected Process, force bool) error {
	current, err := darwinCurrentProcess(ctx, expected.PID)
	if err != nil {
		return err
	}
	if err := validateDarwinIdentity(expected, current); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(expected.PID, signal); err != nil {
		return fmt.Errorf("结束进程失败：%w", err)
	}
	return nil
}

func (*darwinProvider) Details(ctx context.Context, expected Process) (Details, error) {
	current, err := darwinCurrentProcess(ctx, expected.PID)
	if err != nil {
		return Details{}, err
	}
	// 查看自己的环境可以正常进行，禁止终止自身的规则仅用于操作入口。
	if expected.PID != current.PID || expected.Started == "" || expected.Started != current.Started || (expected.Path != "" && expected.Path != current.Path) {
		return Details{}, errors.New("进程身份已变化，请刷新列表后重试")
	}
	result := Details{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Environment, err = readDarwinEnvironment(current.PID)
	if err != nil {
		result.Warning = "目标进程环境不可读取（可能需要同一用户或更高权限）：" + err.Error()
	}
	if current.PPID > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.ParentEnvironment, err = readDarwinEnvironment(current.PPID)
		if err != nil {
			result.Warning = joinDarwinWarnings(result.Warning, "父进程环境不可读取（可能需要同一用户或更高权限）："+err.Error())
		}
	}
	return result, nil
}

func readDarwinEnvironment(pid int) (map[string]string, error) {
	// KERN_PROCARGS2 带有 argc，可精确跳过命令参数，避免把 KEY=VALUE 参数误当环境变量。
	mib := []int32{1, 49, int32(pid)}
	size := uintptr(0)
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), 0, uintptr(unsafe.Pointer(&size)), 0, 0)
	if errno != 0 {
		return nil, errno
	}
	if size < 4 || size > 16*1024*1024 {
		return nil, errors.New("进程环境数据长度异常")
	}
	data := make([]byte, int(size))
	_, _, errno = syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, 0)
	if errno != 0 {
		return nil, errno
	}
	return parseDarwinEnvironment(data[:int(size)])
}

func parseDarwinEnvironment(data []byte) (map[string]string, error) {
	if len(data) < 4 {
		return nil, errors.New("进程环境数据不完整")
	}
	argc := int(binary.LittleEndian.Uint32(data[:4]))
	if argc < 1 || argc > len(data) {
		return nil, errors.New("进程参数数量异常")
	}
	data = data[4:]
	executableEnd := bytes.IndexByte(data, 0)
	if executableEnd < 0 {
		return nil, errors.New("缺少程序路径终止符")
	}
	data = bytes.TrimLeft(data[executableEnd+1:], "\x00")
	for i := 0; i < argc; i++ {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, errors.New("进程参数数据不完整")
		}
		data = data[end+1:]
	}
	result := make(map[string]string)
	for _, entry := range bytes.Split(data, []byte{0}) {
		key, value, ok := strings.Cut(string(entry), "=")
		if ok && key != "" {
			result[key] = value
		}
	}
	return result, nil
}
