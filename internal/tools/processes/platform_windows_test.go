//go:build windows

package processes

import (
	"encoding/binary"
	"testing"
	"time"
	"unicode/utf16"
)

func TestWindowsCPUPercent(t *testing.T) {
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	previous := windowsSample{started: "同一进程", ticks: 10_000_000, at: now.Add(-2 * time.Second)}
	cases := []struct {
		name     string
		previous windowsSample
		current  windowsRawProcess
		want     float64
	}{
		{"一个核心占用一半", previous, windowsRawProcess{Started: "同一进程", Ticks: 20_000_000}, 50},
		{"多核心占用", previous, windowsRawProcess{Started: "同一进程", Ticks: 50_000_000}, 200},
		{"首次采样", windowsSample{}, windowsRawProcess{Started: "新进程", Ticks: 50_000_000}, 0},
		{"PID 被复用", previous, windowsRawProcess{Started: "新进程", Ticks: 50_000_000}, 0},
		{"计数回退", previous, windowsRawProcess{Started: "同一进程", Ticks: 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsCPUPercent(tc.previous, tc.current, now); got != tc.want {
				t.Fatalf("CPU 占用率 = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestVerifiedWindowsProcessRejectsUnsafeTargets(t *testing.T) {
	for _, process := range []Process{{PID: 0}, {PID: 4}, {PID: 12345}} {
		if handle, err := verifiedWindowsProcess(process, 0); err == nil {
			t.Fatalf("不安全或缺少身份信息的进程应被拒绝：%+v，句柄 %v", process, handle)
		}
	}
}

func TestParseWindowsEnvironment(t *testing.T) {
	encode := func(value string) []byte {
		units := utf16.Encode([]rune(value))
		data := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(data[i*2:], unit)
		}
		return data
	}
	data := encode("HTTP_PROXY=http://127.0.0.1:7890\x00名称=中文值\x00TOKEN=a=b\x00EMPTY=\x00=C:=C:\\工作\x00\x00")
	values, err := parseWindowsEnvironment(data)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"HTTP_PROXY": "http://127.0.0.1:7890", "名称": "中文值", "TOKEN": "a=b", "EMPTY": "", "=C:": "C:\\工作"} {
		if got, ok := values[key]; !ok || got != want {
			t.Fatalf("环境变量 %q = %q，期望 %q", key, got, want)
		}
	}
	if _, err := parseWindowsEnvironment(encode("KEY=value\x00")); err == nil {
		t.Fatal("缺少双空字符结束标记应被拒绝")
	}
	if _, err := parseWindowsEnvironment(encode("非法条目\x00\x00")); err == nil {
		t.Fatal("非法环境变量应被拒绝")
	}
	empty, err := parseWindowsEnvironment(encode("\x00\x00"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("读取空环境失败：%v", err)
	}
}
