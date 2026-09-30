//go:build darwin

package processes

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestParseDarwinProcesses(t *testing.T) {
	got, totals := parseDarwinProcessesAndTimes(" 123 1 1:02.50 100 kevin Wed Sep 30 09:09:01 2026 /Applications/My App/run  --port 8080\n")
	if len(got) != 1 {
		t.Fatalf("进程数量：%d", len(got))
	}
	p := got[0]
	if p.PID != 123 || p.Memory != 102400 || totals[123] != 62500*time.Millisecond || p.Started != "Wed Sep 30 09:09:01 2026" || p.Command != "/Applications/My App/run  --port 8080" {
		t.Fatalf("进程字段解析错误：%+v", p)
	}
}

func TestDarwinCPUSampling(t *testing.T) {
	provider := &darwinProvider{}
	processes := []Process{{PID: 123, Started: "first", Path: "/bin/app"}}
	now := time.Now()
	if !provider.sampleCPU(processes, map[int]time.Duration{123: time.Second}, now) || processes[0].CPU != 0 {
		t.Fatal("首帧必须等待 CPU 基线")
	}
	provider.sampleCPU(processes, map[int]time.Duration{123: 4 * time.Second}, now.Add(2*time.Second))
	if processes[0].CPU != 150 {
		t.Fatalf("多核 CPU 差值：%v", processes[0].CPU)
	}
	processes = []Process{{PID: 123, Started: "replacement", Path: "/bin/app"}}
	provider.sampleCPU(processes, map[int]time.Duration{123: 8 * time.Second}, now.Add(4*time.Second))
	if processes[0].CPU != 0 {
		t.Fatal("PID 重用必须清除 CPU 基线")
	}
	provider.sampleCPU(nil, nil, now.Add(6*time.Second))
	if len(provider.previous) != 0 {
		t.Fatal("已退出进程的基线必须删除")
	}
}

func TestParseDarwinPorts(t *testing.T) {
	got := parseDarwinPorts("p123\nf7\nPTCP\nn[::1]:8080\nTST=LISTEN\nf8\nPTCP\nn[::1]:8080\nTST=LISTEN\nf9\nPTCP\nn127.0.0.1:5555->127.0.0.1:80\nTST=ESTABLISHED\nf10\nPUDP\nn*:5353\np234\nf11\nPUDP\nn*:*\n")
	if len(got[123]) != 3 {
		t.Fatalf("未合并重复端口：%+v", got[123])
	}
	if got[123][0].Number != 8080 || got[123][0].State != "LISTEN" || got[123][1].Remote != "127.0.0.1:80" || got[123][2].State != "BOUND" {
		t.Fatalf("端口解析错误：%+v", got[123])
	}
	if got[234][0].Number != 0 {
		t.Fatalf("通配端口应为未知：%+v", got[234])
	}
}

func TestParseDarwinEnvironmentSkipsArguments(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 3)
	data = append(data, []byte("/bin/app\x00\x00\x00app\x00HTTP_PROXY=argument\x00\x00HTTP_PROXY=http://localhost:7890\x00EMPTY=\x00VALUE=a=b c\x00\x00")...)
	got, err := parseDarwinEnvironment(data)
	if err != nil {
		t.Fatal(err)
	}
	if got["HTTP_PROXY"] != "http://localhost:7890" || got["VALUE"] != "a=b c" || len(got) != 3 {
		t.Fatalf("环境解析错误：%+v", got)
	}
	if _, err := parseDarwinEnvironment([]byte{1, 0, 0, 0, 'x'}); err == nil {
		t.Fatal("应拒绝不完整环境数据")
	}
}

func TestDarwinIdentity(t *testing.T) {
	expected := Process{PID: 123456, Path: "/bin/app", Started: "Wed Sep 30 09:09:01 2026"}
	if err := validateDarwinIdentity(expected, expected); err != nil {
		t.Fatal(err)
	}
	changed := expected
	changed.Started = "Wed Sep 30 09:10:01 2026"
	if validateDarwinIdentity(expected, changed) == nil {
		t.Fatal("应拒绝 PID 重用")
	}
	changed = expected
	changed.Path = "/bin/other"
	if validateDarwinIdentity(expected, changed) == nil {
		t.Fatal("应拒绝程序路径变化")
	}
	changed = expected
	changed.PID = os.Getpid()
	if validateDarwinIdentity(changed, changed) == nil {
		t.Fatal("应拒绝结束自身")
	}
}

func TestReadDarwinOwnEnvironment(t *testing.T) {
	// 从系统读取当前测试进程，验证真实 KERN_PROCARGS2 布局与解析一致。
	env, err := readDarwinEnvironment(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if env["PATH"] != os.Getenv("PATH") {
		t.Fatal("读取的 PATH 与当前进程不一致")
	}
	current, err := darwinCurrentProcess(context.Background(), os.Getpid())
	if err != nil || current.Path == "" || current.Started == "" {
		t.Fatalf("进程身份读取失败：%v", err)
	}
}

func TestDarwinSnapshotPortsAndTerminate(t *testing.T) {
	// 只创建本机随机监听端口和专用子进程，避免碰触已有服务。
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("创建测试监听端口失败")
	}
	t.Cleanup(func() { _ = listener.Close() })
	portNumber := listener.Addr().(*net.TCPAddr).Port
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal("启动测试子进程失败")
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = child.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
			return
		default:
			_ = child.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("清理测试子进程超时")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	provider := NewProvider()
	snapshot, err := provider.Snapshot(ctx)
	if err != nil {
		t.Fatal("读取真实进程快照失败")
	}
	var target Process
	foundListener := false
	for _, process := range snapshot.Processes {
		if process.PID == child.Process.Pid {
			target = process
		}
		if process.PID == os.Getpid() {
			for _, port := range process.Ports {
				if port.Protocol == "TCP" && port.State == "LISTEN" && port.Number == portNumber {
					foundListener = true
				}
			}
		}
	}
	if !foundListener {
		t.Fatal("真实进程快照未找到当前测试进程的监听端口")
	}
	if target.PID == 0 || target.Path == "" || target.Started == "" {
		t.Fatal("真实进程快照未取得测试子进程的完整身份")
	}
	if err := provider.Terminate(ctx, target, false); err != nil {
		t.Fatal("安全结束测试子进程失败")
	}
	select {
	case <-done:
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) {
			t.Fatal("测试子进程未因终止信号退出")
		}
		status, ok := exit.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
			t.Fatal("测试子进程未收到正常终止信号")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待测试子进程结束超时")
	}
}
