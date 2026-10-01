package services

import (
	"strings"
	"testing"
)

func TestStartupMigrationAndQuoting(t *testing.T) {
	item := Item{Startup: true, Name: "test'item", Domain: "test'item", Path: `HKCU:\Software\Microsoft\Windows\CurrentVersion\Run`, Command: `C:\app.exe`}
	actions := Actions(item)
	if len(actions) != 1 || actions[0].Key != "d" {
		t.Fatal("启用的 Run 项应提供禁用操作")
	}
	script := actions[0].Command.Args[len(actions[0].Command.Args)-1]
	if !strings.Contains(script, "test''item") || !strings.Contains(script, "SysboxDisabledRun") || !strings.Contains(script, "启动项已变化") {
		t.Fatal("迁移应转义名称并重新检查源值")
	}
	item.Path = `HKCU:\Software\Microsoft\Windows\CurrentVersion\SysboxDisabledRun`
	if actions := Actions(item); len(actions) != 1 || actions[0].Key != "e" {
		t.Fatal("禁用的 Run 项应提供启用操作")
	}
	if len(Actions(Item{Startup: true, Path: `C:\Startup\app.lnk`})) != 0 {
		t.Fatal("启动文件夹项目不应提供注册表操作")
	}
}
