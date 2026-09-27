// Package appstore 处理 appstoreagent 的 CPU 死循环。
//
// 部分 macOS 版本升级后，appstoreagent 会陷入 com.apple.appstored.ArcadeManager.dispatch
// 死循环，持续占满一个核，并拖累 dasd、analyticsd 等进程。结束进程、重启、重登 App Store
// 都会复发，只能禁用它的自动拉起，等 Apple 修复后再恢复。
//
// 禁用后失去 App Store 后台自动更新检查；App Store.app 的浏览、购买和手动更新不受影响。
// launchctl disable 对按需拉起的服务抑制有限，XPC 事件仍可能重新拉起进程。
package appstore

import (
	"context"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

const (
	procName = "appstoreagent"
	label    = "com.apple.appstoreagent"
	plist    = "/System/Library/LaunchAgents/com.apple.appstoreagent.plist"
)

// 判定死循环的阈值：运行超过 30 秒，且累计 CPU 时间超过运行时长的 40%
const (
	loopMinElapsed = 30 * time.Second
	loopRatio      = 0.4
)

// Status 当前状态
type Status struct {
	Disabled bool        // 自动拉起已禁用
	Procs    []sysx.Proc // 正在运行的进程
}

// Looping 进程是否疑似陷入死循环
func Looping(p sysx.Proc) bool {
	return p.Elapsed > loopMinElapsed && p.BusyRatio() > loopRatio
}

// Service 对 appstoreagent 的操作
type Service struct {
	Runner sysx.Runner
}

// New 使用真实命令执行器
func New() *Service { return &Service{Runner: sysx.ExecRunner{}} }

// Status 查询禁用标记与进程
func (s *Service) Status(ctx context.Context) (Status, error) {
	var st Status
	disabled, err := sysx.DisabledMap(ctx, s.Runner, sysx.GUIDomain())
	if err != nil {
		return st, err
	}
	st.Disabled = disabled[label]
	st.Procs, err = sysx.FindProcs(ctx, s.Runner, sysx.ByName(procName))
	return st, err
}

// Disable 禁用自动拉起（持久化，重启后仍生效）并结束当前进程
func (s *Service) Disable(ctx context.Context) []sysx.Step {
	rec := &sysx.Recorder{Runner: s.Runner}
	domain := sysx.GUIDomain()
	_ = rec.Do(ctx, "禁用自动拉起", sysx.C("launchctl", "disable", domain+"/"+label), false)
	// 卸载服务定义；受 SIP 保护时会失败，属正常情况
	_ = rec.Do(ctx, "卸载服务定义", sysx.C("launchctl", "bootout", domain+"/"+label), true)

	procs, _ := sysx.FindProcs(ctx, s.Runner, sysx.ByName(procName))
	if len(procs) == 0 {
		rec.Note("当前没有运行中的进程", nil)
		return rec.Steps
	}
	pids := sysx.PIDs(procs)
	if alive := sysx.Terminate(ctx, s.Runner, pids, 2*time.Second, false); len(alive) > 0 {
		rec.Note("结束进程", sysx.AliveError{PIDs: alive})
	} else {
		rec.Note("已结束进程 "+sysx.JoinPIDs(pids), nil)
	}
	return rec.Steps
}

// Enable 清除禁用标记并重新拉起，供 Apple 修复后恢复默认行为
func (s *Service) Enable(ctx context.Context) []sysx.Step {
	rec := &sysx.Recorder{Runner: s.Runner}
	domain := sysx.GUIDomain()
	_ = rec.Do(ctx, "清除禁用标记", sysx.C("launchctl", "enable", domain+"/"+label), false)
	// 服务可能已加载，bootstrap 失败时再用 kickstart 拉起
	if rec.Do(ctx, "加载服务定义", sysx.C("launchctl", "bootstrap", domain, plist), true) != nil {
		_ = rec.Do(ctx, "拉起服务", sysx.C("launchctl", "kickstart", domain+"/"+label), true)
	}
	return rec.Steps
}
