package services

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodeElevated(t *testing.T, script string) string {
	t.Helper()
	start := strings.Index(script, "-EncodedCommand '") + len("-EncodedCommand '")
	end := strings.Index(script[start:], "'")
	raw, err := base64.StdEncoding.DecodeString(script[start : start+end])
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(units))
}

func TestStartupApprovedToggle(t *testing.T) {
	item := Item{Kind: "run", Name: "test'item", Domain: "test'item", Scope: "HKCU", Approval: `HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`}
	decorateStartup(&item)
	actions := Actions(item)
	if len(actions) != 1 || actions[0].Label != "禁用" || !strings.Contains(actions[0].Preview, "test''item") || !strings.Contains(actions[0].Preview, "(3,0,") {
		t.Fatalf("启用的启动项应写入 StartupApproved 禁用标记：%+v", actions)
	}
	item.Disabled = true
	if actions := Actions(item); actions[0].Label != "启用" || !strings.Contains(actions[0].Preview, "(2,0,") {
		t.Fatal("禁用的启动项应可启用")
	}
}

func TestServiceActionsElevate(t *testing.T) {
	item := Item{ID: "Spooler", Name: "Print Spooler", State: "Running", Mode: "Auto", Command: `C:\Windows\System32\spoolsv.exe`}
	decorateService(&item)
	actions := Actions(item)
	got := ""
	for _, a := range actions {
		got += a.Key
	}
	if got != "sRmdl" || item.Source != SourceSystem {
		t.Fatalf("运行中的自动服务应提供停止、重启与改为手动或禁用：%s", got)
	}
	script := actions[0].Command.Args[len(actions[0].Command.Args)-1]
	if !strings.Contains(script, "-Verb RunAs") || !strings.Contains(decodeElevated(t, script), "Stop-Service -Name 'Spooler'") {
		t.Fatal("服务操作应在需要时请求管理员授权")
	}
	stopped := Item{ID: "x", State: "Stopped", Mode: "Auto"}
	decorateService(&stopped)
	if !stopped.Failed || stopped.State != "应运行未运行" {
		t.Fatal("自动启动但未运行的服务应标记为异常")
	}
}
