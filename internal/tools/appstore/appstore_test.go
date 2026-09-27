package appstore

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
	"github.com/KevinXC5/sysbox/internal/sysx/sysxtest"
)

func TestStatus(t *testing.T) {
	r := &sysxtest.Runner{Replies: map[string]sysxtest.Reply{
		"launchctl print-disabled": {Out: `"com.apple.appstoreagent" => disabled`},
		"ps ": {Out: "41771 99.0 30:00.00 50:00 15024 R /System/Library/PrivateFrameworks/AppStoreDaemon.framework/Support/appstoreagent\n" +
			"500 0.0 0:01.00 50:00 100 S /usr/bin/other\n"},
	}}
	st, err := (&Service{Runner: r}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Disabled || len(st.Procs) != 1 {
		t.Fatalf("状态解析有误：%+v", st)
	}
	if !Looping(st.Procs[0]) {
		t.Error("CPU 时间占运行时长 60%，应判定为疑似死循环")
	}
}

func TestLoopingThreshold(t *testing.T) {
	short := sysx.Proc{CPUTime: 20 * time.Second, Elapsed: 25 * time.Second}
	idle := sysx.Proc{CPUTime: time.Second, Elapsed: time.Hour}
	if Looping(short) || Looping(idle) {
		t.Error("运行不足 30 秒或占比低时不应判定为死循环")
	}
}

// 用一个真实的子进程冒充 appstoreagent，验证禁用流程会下发命令并结束进程
func TestDisable(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()

	r := &sysxtest.Runner{Replies: map[string]sysxtest.Reply{
		"ps ":               {Out: fmt.Sprintf("%d 99.0 1:00.00 2:00 100 R /x/appstoreagent\n", child.Process.Pid)},
		"launchctl bootout": {Err: sysxtest.ErrFailed},
	}, Passthrough: []string{"kill "}}
	steps := (&Service{Runner: r}).Disable(context.Background())

	if !r.Called("launchctl disable gui/") {
		t.Error("应下发 launchctl disable")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("冒充的进程应被结束")
	}
	last := steps[len(steps)-1]
	if last.Err != nil {
		t.Errorf("结束进程不应报错：%v", last.Err)
	}
	// bootout 失败属正常情况，应标记为 Soft
	for _, s := range steps {
		if s.Title == "卸载服务定义" && (!s.Soft || s.Err == nil) {
			t.Errorf("bootout 步骤记录有误：%+v", s)
		}
	}
}
