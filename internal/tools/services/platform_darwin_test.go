package services

import (
	"context"
	"errors"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainAndDisabledParsing(t *testing.T) {
	items := decodeDomain("system = {\n services = {\n 42 - com.example.running\n 0 1 com.example.failed\n 0 (pe) com.example.waiting\n }\n}", "system")
	if len(items) != 3 || items[0].PID != 42 || items[1].State != "已退出（1）" || items[2].State != "等待触发" {
		t.Fatalf("服务状态解析错误：%+v", items)
	}
	flags := decodeDisabled("disabled services = {\n\"com.example.running\" => false\n\"com.example.failed\" => true\n}")
	if flags["com.example.running"] || !flags["com.example.failed"] {
		t.Fatal("启用状态解析错误")
	}
}
func TestStartupActions(t *testing.T) {
	item, e := decodePlist(`{"Label":"com.example.agent","ProgramArguments":["/opt/my app/bin","--listen"],"RunAtLoad":true,"StandardOutPath":"/tmp/agent.log"}`, "/Library/LaunchAgents/agent.plist", "gui/501")
	if e != nil {
		t.Fatal(e)
	}
	item.State = "未加载"
	actions := Actions(item)
	if actions[0].Command.Args[0] != "bootstrap" || actions[0].Command.Args[2] != item.Path {
		t.Fatal("未加载服务应使用配置文件启动")
	}
	for _, a := range actions {
		if a.Key == "d" && !strings.Contains(a.Note, "不会立即停止") {
			t.Fatal("禁用说明应区分运行状态")
		}
		if a.Key == "l" && a.Command.Name != "tail" {
			t.Fatal("应优先读取配置日志")
		}
	}
}

func TestPlaceholderAndInvalidPlists(t *testing.T) {
	for _, text := range []string{"{}", "{\n  \n}"} {
		if _, err := decodePlist(text, "/tmp/placeholder.plist", "gui/501"); !errors.Is(err, errPlaceholderPlist) {
			t.Fatalf("合法空字典应识别为占位文件：%v", err)
		}
	}
	for _, text := range []string{`null`, `[]`, `{"Program":"/bin/sleep"}`, `{"Label":42}`} {
		if _, err := decodePlist(text, "/tmp/invalid.plist", "gui/501"); err == nil || errors.Is(err, errPlaceholderPlist) {
			t.Fatalf("无效配置应保留真实错误：%s，%v", text, err)
		}
	}
}

func TestPlutilEmptyPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "com.example.old-updater.plist")
	text := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict/></plist>`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := (sysx.ExecRunner{}).Run(context.Background(), sysx.C("plutil", "-convert", "json", "-o", "-", "--", path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePlist(out, path, "gui/501"); !errors.Is(err, errPlaceholderPlist) {
		t.Fatalf("真实 plutil 输出的占位文件不应报解析失败：%v", err)
	}
}
