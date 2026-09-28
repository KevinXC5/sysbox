package sysx

import (
	"runtime"
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
}

func TestCmdString(t *testing.T) {
	c := C("launchctl", "bootout", "system", "/Library/Launch Daemons/a.plist")
	want := `launchctl bootout system "/Library/Launch Daemons/a.plist"`
	if c.String() != want {
		t.Errorf("命令文本 %q，期望 %q", c.String(), want)
	}
}

func TestExeName(t *testing.T) {
	if runtime.GOOS == "windows" {
		if exeName("Claude.EXE") != "claude" || exeName(`C:\Tools\idea64.exe`) != "idea64" {
			t.Fatal("Windows 应去掉 .exe 并忽略大小写")
		}
		if exeName("claude") != "claude" {
			t.Fatal("不带后缀的名字应保持小写")
		}
		return
	}
	if exeName("claude") != "claude" || exeName("Claude.EXE") != "Claude.EXE" {
		t.Fatal("非 Windows 不应改写进程名")
	}
}

func TestByNameWindowsExe(t *testing.T) {
	match := ByName("claude", "idea")
	hit := Proc{Path: "claude"}
	miss := Proc{Path: "codex"}
	if runtime.GOOS == "windows" {
		if !match(Proc{Path: "Claude.EXE"}) {
			t.Fatal("Windows 上 Claude.EXE 应匹配 claude")
		}
		if match(Proc{Path: "idea64.exe"}) {
			t.Fatal("idea64.exe 不应匹配 idea")
		}
		return
	}
	if !match(hit) || match(miss) {
		t.Fatal("非 Windows 应按原名精确匹配")
	}
	if match(Proc{Path: "Claude.EXE"}) {
		t.Fatal("非 Windows 不应忽略大小写或后缀")
	}
}
