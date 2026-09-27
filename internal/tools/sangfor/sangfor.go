// Package sangfor 管理深信服 aTrust 与 VDI 客户端的启停。
//
// 在部分 macOS 版本上，深信服客户端会导致 ecosystemd 持续高 CPU。停止时卸载并永久禁用
// 全部 launchd 服务、结束残留进程并重启 ecosystemd；启动时按依赖顺序解禁并重新加载。
// 服务列表从 /Library/LaunchDaemons 和 /Library/LaunchAgents 下的 com.sangfor.*.plist 动态发现，
// 进程按 plist 登记的程序路径和客户端安装目录精确识别，不做模糊匹配。
package sangfor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Service 一个 launchd 服务
type Service struct {
	Label    string
	Plist    string
	System   bool     // LaunchDaemon 属于 system 域，LaunchAgent 属于当前用户的 gui 域
	Programs []string // plist 登记的可执行文件
	Loaded   bool
	Disabled bool
}

// Domain 服务所在的 launchd 域
func (s Service) Domain() string {
	if s.System {
		return sysx.SystemDomain
	}
	return sysx.GUIDomain()
}

// Status 客户端当前状态
type Status struct {
	Services []Service
	Procs    []sysx.Proc // 深信服相关进程
	Eco      []sysx.Proc // ecosystemd
}

// Running 是否还有深信服进程在运行
func (s Status) Running() bool { return len(s.Procs) > 0 }

// startOrder 启动时的依赖顺序，未列出的服务排在后面
var startOrder = []string{
	"com.sangfor.aTrustTunnel",
	"com.sangfor.CSMonitor",
	"com.sangfor.limit.maxfiles",
	"com.sangfor.aTrustUninstallMonitor",
	"com.sangfor.aTrustDaemon",
	"com.sangfor.aTrustCore",
	"com.sangfor.aTrustTray",
	"com.sangfor.CSAgentProxy",
}

// Client 深信服客户端操作
type Client struct {
	Runner    sysx.Runner
	DaemonDir string
	AgentDir  string
	// InstallDirs 客户端安装目录，位于其中的进程都视为深信服进程
	InstallDirs []string
}

// New 使用 macOS 默认路径
func New() *Client {
	return &Client{
		Runner:    sysx.ExecRunner{},
		DaemonDir: "/Library/LaunchDaemons",
		AgentDir:  "/Library/LaunchAgents",
		InstallDirs: []string{
			"/Applications/aTrust.app/",
			"/Applications/SangforVDIClient.app/",
			"/Library/Application Support/aTrust/",
			"/Library/Application Support/Sangfor/",
		},
	}
}

// Discover 发现全部深信服服务，并读取各自登记的程序路径
func (c *Client) Discover(ctx context.Context) []Service {
	var svcs []Service
	for _, d := range []struct {
		dir    string
		system bool
	}{{c.DaemonDir, true}, {c.AgentDir, false}} {
		plists, _ := filepath.Glob(filepath.Join(d.dir, "com.sangfor.*.plist"))
		for _, p := range plists {
			svcs = append(svcs, Service{
				Label:    strings.TrimSuffix(filepath.Base(p), ".plist"),
				Plist:    p,
				System:   d.system,
				Programs: c.programs(ctx, p),
			})
		}
	}
	rank := func(label string) int {
		for i, l := range startOrder {
			if l == label {
				return i
			}
		}
		return len(startOrder)
	}
	sort.SliceStable(svcs, func(i, j int) bool {
		// 系统守护在前，其次按依赖顺序，最后按名称
		if svcs[i].System != svcs[j].System {
			return svcs[i].System
		}
		if ri, rj := rank(svcs[i].Label), rank(svcs[j].Label); ri != rj {
			return ri < rj
		}
		return svcs[i].Label < svcs[j].Label
	})
	return svcs
}

// programs 用 plutil 把 plist 转成 JSON，读取 Program 与 ProgramArguments[0]
func (c *Client) programs(ctx context.Context, plist string) []string {
	out, err := c.Runner.Run(ctx, sysx.C("plutil", "-convert", "json", "-o", "-", plist))
	if err != nil {
		return nil
	}
	var v struct {
		Program          string   `json:"Program"`
		ProgramArguments []string `json:"ProgramArguments"`
	}
	if json.Unmarshal([]byte(out), &v) != nil {
		return nil
	}
	var progs []string
	if v.Program != "" {
		progs = append(progs, v.Program)
	}
	if len(v.ProgramArguments) > 0 && v.ProgramArguments[0] != v.Program {
		progs = append(progs, v.ProgramArguments[0])
	}
	return progs
}

// Status 查询服务加载与禁用状态、相关进程和 ecosystemd
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	st.Services = c.Discover(ctx)
	sysDisabled, _ := sysx.DisabledMap(ctx, c.Runner, sysx.SystemDomain)
	guiDisabled, _ := sysx.DisabledMap(ctx, c.Runner, sysx.GUIDomain())
	programs := map[string]bool{}
	for i := range st.Services {
		s := &st.Services[i]
		s.Loaded = sysx.Loaded(ctx, c.Runner, s.Domain(), s.Label)
		if s.System {
			s.Disabled = sysDisabled[s.Label]
		} else {
			s.Disabled = guiDisabled[s.Label]
		}
		for _, p := range s.Programs {
			programs[p] = true
		}
	}

	procs, err := sysx.Procs(ctx, c.Runner)
	if err != nil {
		return st, err
	}
	for _, p := range procs {
		switch {
		case p.Name() == "ecosystemd":
			st.Eco = append(st.Eco, p)
		case programs[p.Path] || c.inInstallDir(p.Path):
			st.Procs = append(st.Procs, p)
		}
	}
	return st, nil
}

func (c *Client) inInstallDir(path string) bool {
	for _, d := range c.InstallDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// Stop 卸载并永久禁用全部服务，结束残留进程，再重启 ecosystemd 清除其缓存状态。
// 需要事先通过 sudo -v 缓存凭据。
func (c *Client) Stop(ctx context.Context) []sysx.Step {
	rec := &sysx.Recorder{Runner: c.Runner}
	for _, s := range c.Discover(ctx) {
		if s.System {
			_ = rec.Do(ctx, "停止系统守护 "+s.Label, sysx.Root("launchctl", "bootout", "system", s.Plist), true)
			_ = rec.Do(ctx, "禁用开机自启 "+s.Label, sysx.Root("launchctl", "disable", "system/"+s.Label), false)
			continue
		}
		gui := sysx.GUIDomain()
		_ = rec.Do(ctx, "停止用户代理 "+s.Label, sysx.C("launchctl", "bootout", gui, s.Plist), true)
		_ = rec.Do(ctx, "禁用开机自启 "+s.Label, sysx.C("launchctl", "disable", gui+"/"+s.Label), false)
		// 少数情况下用户代理被注册到了 system 域，一并清理
		if sysx.Loaded(ctx, c.Runner, sysx.SystemDomain, s.Label) {
			_ = rec.Do(ctx, "停止 system 域副本 "+s.Label, sysx.Root("launchctl", "bootout", "system", s.Plist), true)
			_ = rec.Do(ctx, "禁用 system 域副本 "+s.Label, sysx.Root("launchctl", "disable", "system/"+s.Label), true)
		}
	}

	st, err := c.Status(ctx)
	if err != nil {
		rec.Note("查询残留进程", err)
	} else if st.Running() {
		c.terminate(ctx, rec, st.Procs)
	}
	_ = rec.Do(ctx, "重启 ecosystemd", sysx.Root("killall", "ecosystemd"), true)
	return rec.Steps
}

// terminate 以 root 身份结束进程：先 TERM，超时后 KILL
func (c *Client) terminate(ctx context.Context, rec *sysx.Recorder, procs []sysx.Proc) {
	pids := sysx.PIDs(procs)
	if alive := sysx.Terminate(ctx, c.Runner, pids, 3*time.Second, true); len(alive) > 0 {
		rec.Note("结束残留进程", sysx.AliveError{PIDs: alive})
		return
	}
	rec.Note("结束残留进程 "+sysx.JoinPIDs(pids), nil)
}

// Start 按依赖顺序解除禁用并加载全部服务，恢复开机自启。需要事先缓存 sudo 凭据。
func (c *Client) Start(ctx context.Context) []sysx.Step {
	rec := &sysx.Recorder{Runner: c.Runner}
	sysDisabled, _ := sysx.DisabledMap(ctx, c.Runner, sysx.SystemDomain)
	for _, s := range c.Discover(ctx) {
		if s.System {
			_ = rec.Do(ctx, "解除禁用 "+s.Label, sysx.Root("launchctl", "enable", "system/"+s.Label), false)
			// 已加载时 bootstrap 会报错，属正常情况
			_ = rec.Do(ctx, "加载系统守护 "+s.Label, sysx.Root("launchctl", "bootstrap", "system", s.Plist), true)
			continue
		}
		gui := sysx.GUIDomain()
		_ = rec.Do(ctx, "解除禁用 "+s.Label, sysx.C("launchctl", "enable", gui+"/"+s.Label), false)
		if sysDisabled[s.Label] {
			_ = rec.Do(ctx, "解除 system 域禁用 "+s.Label, sysx.Root("launchctl", "enable", "system/"+s.Label), true)
		}
		// 用户代理只加载到 gui 域；加载到 system 域会以 root 身份再起一份
		_ = rec.Do(ctx, "加载用户代理 "+s.Label, sysx.C("launchctl", "bootstrap", gui, s.Plist), true)
	}
	return rec.Steps
}
