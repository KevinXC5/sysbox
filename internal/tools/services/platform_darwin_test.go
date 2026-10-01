package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func TestDomainAndDisabledParsing(t *testing.T) {
	items := decodeDomain("system = {\n services = {\n 42 - com.example.running\n 0 1 com.example.failed\n 0 (pe) com.example.waiting\n 0 -15 com.example.stopped\n }\n}", "system")
	if len(items) != 4 || !items[0].Running || !items[1].Failed || items[2].Failed || items[3].Failed || items[3].Running {
		t.Fatalf("服务状态解析错误：%+v", items)
	}
	flags := decodeDisabled("disabled services = {\n\"com.example.running\" => false\n\"com.example.failed\" => true\n}")
	if flags["com.example.running"] || !flags["com.example.failed"] {
		t.Fatal("启用状态解析错误")
	}
}

func TestClassifyAndAppName(t *testing.T) {
	for label, want := range map[string]string{"com.apple.Finder": SourceApple, "application.com.tencent.xinWeChat.396326254.396326260": SourceApp, "homebrew.mxcl.redis": SourceThirdParty} {
		item := Item{Name: label, Domain: "gui/501"}
		classify(&item)
		if item.Source != want {
			t.Fatalf("%s 来源应为 %s，实际 %s", label, want, item.Source)
		}
	}
	if appName("application.com.tencent.xinWeChat.396326254.396326260") != "com.tencent.xinWeChat" {
		t.Fatal("应用实例应还原为应用标识")
	}
}

func keys(actions []Action) string {
	var out []string
	for _, a := range actions {
		out = append(out, a.Key)
	}
	return strings.Join(out, "")
}

func TestActionsFollowState(t *testing.T) {
	item, e := decodePlist(`{"Label":"com.example.agent","ProgramArguments":["/opt/my app/bin","--listen"],"RunAtLoad":true,"KeepAlive":true,"StandardOutPath":"/tmp/agent.log"}`, "/Library/LaunchAgents/agent.plist", "gui/501")
	if e != nil {
		t.Fatal(e)
	}
	classify(&item)
	item.State = "未加载"
	actions := Actions(item)
	if keys(actions) != "seol" || actions[0].Command.Args[0] != "bootstrap" || actions[1].Label != "禁用" {
		t.Fatalf("未加载服务应可从配置启动并禁用：%s", keys(actions))
	}
	item.State, item.Running = "运行中", true
	actions = Actions(item)
	if keys(actions) != "sReovl" || actions[0].Command.Args[0] != "bootout" {
		t.Fatalf("运行中的服务应提供停止与重启：%s", keys(actions))
	}
	apple := Item{Name: "com.apple.Finder", Domain: "gui/501", State: "运行中", Running: true}
	classify(&apple)
	if keys(Actions(apple)) != "vl" {
		t.Fatal("Apple 服务只能查看")
	}
	if keys(Actions(Item{Kind: "login", Name: "App", Path: "/Applications/App.app"})) != "do" {
		t.Fatal("登录项应提供移除与显示")
	}
}

func TestAdminCommandQuoting(t *testing.T) {
	cmd := asAdmin(sysx.C("launchctl", "bootout", `system/it's "x"`))
	script := cmd.Args[1]
	if cmd.Name != "osascript" || !strings.Contains(script, `'system/it'\\''s \"x\"'`) || !strings.HasSuffix(script, "with administrator privileges") {
		t.Fatalf("管理员命令转义错误：%s", script)
	}
}

func TestLoginItemsParsing(t *testing.T) {
	items := decodeLoginItems("Stats\t/Applications/Stats.app\nOrbStack\t/Applications/OrbStack.app\n\n")
	if len(items) != 2 || items[1].Path != "/Applications/OrbStack.app" || !items[0].Startup {
		t.Fatalf("登录项解析错误：%+v", items)
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
