package sangfor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx/sysxtest"
)

// fixture 在临时目录里放几份 plist，并预设 plutil 与 ps 的输出
func fixture(t *testing.T) (*Client, *sysxtest.Runner) {
	t.Helper()
	root := t.TempDir()
	daemons, agents := filepath.Join(root, "LaunchDaemons"), filepath.Join(root, "LaunchAgents")
	for _, p := range []string{
		filepath.Join(daemons, "com.sangfor.CSMonitor.plist"),
		filepath.Join(daemons, "com.sangfor.aTrustTunnel.plist"),
		filepath.Join(agents, "com.sangfor.aTrustTray.plist"),
		filepath.Join(agents, "com.other.thing.plist"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &sysxtest.Runner{Replies: map[string]sysxtest.Reply{
		"plutil -convert json -o - " + filepath.Join(daemons, "com.sangfor.CSMonitor.plist"): {
			Out: `{"Program":"/opt/vdi/CSMonitor"}`},
		"plutil -convert json -o - " + filepath.Join(daemons, "com.sangfor.aTrustTunnel.plist"): {
			Out: `{"ProgramArguments":["/opt/atrust/aTrustAgent","--plugin","tunnel"]}`},
		"ps ": {Out: "99990 1.0 0:01.00 10:00 100 S /opt/vdi/CSMonitor\n" +
			"99991 1.0 0:01.00 10:00 100 S /Applications/aTrust.app/Contents/MacOS/aTrustTray\n" +
			"102 50.0 5:00.00 10:00 100 R /System/Library/PrivateFrameworks/Ecosystem.framework/Support/ecosystemd\n" +
			"103 0.0 0:00.10 10:00 100 S /usr/local/bin/vdi-tool\n"},
		"launchctl print system/com.sangfor.aTrustTray": {Err: sysxtest.ErrFailed},
	}}
	c := &Client{Runner: r, DaemonDir: daemons, AgentDir: agents, InstallDirs: []string{"/Applications/aTrust.app/"}}
	return c, r
}

func TestDiscoverOrder(t *testing.T) {
	c, _ := fixture(t)
	svcs := c.Discover(context.Background())
	var labels []string
	for _, s := range svcs {
		labels = append(labels, s.Label)
	}
	want := "com.sangfor.aTrustTunnel,com.sangfor.CSMonitor,com.sangfor.aTrustTray"
	if strings.Join(labels, ",") != want {
		t.Errorf("服务顺序 %v，期望 %s", labels, want)
	}
	if len(svcs[0].Programs) != 1 || svcs[0].Programs[0] != "/opt/atrust/aTrustAgent" {
		t.Errorf("程序路径解析有误：%+v", svcs[0])
	}
}

// 只认 plist 登记的程序和安装目录，名字里带 vdi 的无关进程不应被误判
func TestStatusMatchesPrecisely(t *testing.T) {
	c, _ := fixture(t)
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Procs) != 2 || len(st.Eco) != 1 {
		t.Fatalf("进程识别有误：procs=%+v eco=%+v", st.Procs, st.Eco)
	}
	for _, p := range st.Procs {
		if p.PID == 103 {
			t.Error("无关进程 vdi-tool 被误判为深信服进程")
		}
	}
}

func TestStopCommands(t *testing.T) {
	c, r := fixture(t)
	c.Stop(context.Background())
	for _, want := range []string{
		"sudo launchctl bootout system " + filepath.Join(c.DaemonDir, "com.sangfor.aTrustTunnel.plist"),
		"sudo launchctl disable system/com.sangfor.CSMonitor",
		"launchctl disable gui/",
		"sudo kill -TERM 99990 99991",
		"sudo killall ecosystemd",
	} {
		if !r.Called(want) {
			t.Errorf("缺少命令：%s\n实际：%s", want, strings.Join(r.Calls, "\n"))
		}
	}
	if r.Called("sudo launchctl bootout system " + filepath.Join(c.AgentDir, "com.sangfor.aTrustTray.plist")) {
		t.Error("用户代理未注册到 system 域时，不应尝试从 system 域卸载")
	}
}
