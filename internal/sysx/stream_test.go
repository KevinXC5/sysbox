package sysx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// 测试子进程仅输出固定内容，验证真实管道、stderr、取消和进程退出。
func TestStreamHelperProcess(t *testing.T) {
	mode := os.Args[len(os.Args)-1]
	if !strings.HasPrefix(mode, "sysbox-stream-") {
		return
	}
	fmt.Fprintln(os.Stdout, "stdout-line")
	fmt.Fprintln(os.Stderr, "stderr-line")
	switch mode {
	case "sysbox-stream-wait":
		time.Sleep(30 * time.Second)
	case "sysbox-stream-fail":
		fmt.Fprintln(os.Stderr, "helper-failure")
		os.Exit(7)
	case "sysbox-stream-long":
		fmt.Fprintln(os.Stdout, strings.Repeat("x", 2*1024*1024))
	default:
		fmt.Fprint(os.Stdout, "partial-line")
	}
	os.Exit(0)
}
func streamHelper(t *testing.T, mode string) Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return C(exe, "-test.run=^TestStreamHelperProcess$", "--", "sysbox-stream-"+mode)
}
func TestExecStreamMergedOutput(t *testing.T) {
	var lines []string
	err := (ExecRunner{}).Stream(context.Background(), streamHelper(t, "normal"), func(line string) { lines = append(lines, line) })
	if err != nil || !slices.Contains(lines, "stdout-line") || !slices.Contains(lines, "stderr-line") || !slices.Contains(lines, "partial-line") {
		t.Fatalf("流式输出不完整：%q，%v", lines, err)
	}
}
func TestExecStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	err := (ExecRunner{}).Stream(ctx, streamHelper(t, "wait"), func(string) { cancel() })
	if !errors.Is(err, context.Canceled) || time.Since(started) > 5*time.Second {
		t.Fatalf("取消未及时关闭进程和管道：%v", err)
	}
}
func TestExecStreamFailureAndLongLine(t *testing.T) {
	for _, test := range []struct{ mode, want string }{{"fail", "helper-failure"}, {"long", "读取输出失败"}} {
		err := (ExecRunner{}).Stream(context.Background(), streamHelper(t, test.mode), func(string) {})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("错误信息不明确：%s，%v", test.mode, err)
		}
	}
}
