package services

import (
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
