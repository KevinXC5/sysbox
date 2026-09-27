package sysx

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseClock(t *testing.T) {
	cases := map[string]time.Duration{
		"0:06.40":     6*time.Second + 400*time.Millisecond,
		"23:52:35":    23*time.Hour + 52*time.Minute + 35*time.Second,
		"2-01:00:00":  49 * time.Hour,
		"12:05":       12*time.Minute + 5*time.Second,
		"not-a-clock": 0,
	}
	for in, want := range cases {
		if got := ParseClock(in); got != want {
			t.Errorf("ParseClock(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestParsePS(t *testing.T) {
	out := "41771   0.0   0:06.40 23:52:35 15024 S     /System/Library/AppStoreDaemon.framework/Support/appstoreagent\n" +
		"  1261  97.5  10:00.00    20:00  2048 R     /Applications/My App.app/Contents/MacOS/My App\n" +
		"garbage line\n"
	procs := ParsePS(out)
	if len(procs) != 2 {
		t.Fatalf("期望 2 个进程，实际 %d", len(procs))
	}
	if procs[0].Name() != "appstoreagent" || procs[0].RSS != 15024*1024 {
		t.Errorf("第一个进程解析有误：%+v", procs[0])
	}
	if procs[1].Path != "/Applications/My App.app/Contents/MacOS/My App" {
		t.Errorf("含空格的路径解析有误：%q", procs[1].Path)
	}
	if r := procs[1].BusyRatio(); r < 0.49 || r > 0.51 {
		t.Errorf("CPU 占比计算有误：%v", r)
	}
}

func TestParseDisabled(t *testing.T) {
	out := `	disabled services = {
		"com.sangfor.aTrustTray" => disabled
		"com.logi.optionsplus" => enabled
		"com.apple.appstoreagent" => true
	}`
	m := ParseDisabled(out)
	if !m["com.sangfor.aTrustTray"] || m["com.logi.optionsplus"] || !m["com.apple.appstoreagent"] {
		t.Errorf("禁用标记解析有误：%v", m)
	}
}

func TestCmdString(t *testing.T) {
	c := Root("launchctl", "bootout", "system", "/Library/Launch Daemons/a.plist")
	want := `sudo launchctl bootout system "/Library/Launch Daemons/a.plist"`
	if c.String() != want {
		t.Errorf("命令文本 %q，期望 %q", c.String(), want)
	}
}

// 演练执行器：只读命令真实执行，修改类命令只记录；Terminate 不会真的发信号
func TestDryRunner(t *testing.T) {
	d := NewDryRunner(ExecRunner{})
	out, err := d.Run(context.Background(), C("sysctl", "-n", "hw.ncpu"))
	if err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("只读命令应真实执行：%q %v", out, err)
	}
	if _, err := d.Run(context.Background(), Root("launchctl", "disable", "system/x")); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	if alive := Terminate(context.Background(), d, []int{child.Process.Pid}, time.Second, false); alive != nil {
		t.Errorf("演练模式应视为全部结束：%v", alive)
	}
	if !Alive(child.Process.Pid) {
		t.Error("演练模式不应真的结束进程")
	}
	calls := d.Calls()
	if len(calls) != 2 || calls[0] != "sudo launchctl disable system/x" || !strings.HasPrefix(calls[1], "kill -TERM ") {
		t.Errorf("记录的命令有误：%v", calls)
	}
}
