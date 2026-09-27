// Package cursorui 诊断 CursorUIViewService（系统文本输入光标旁的 UI 服务）卡顿或占用过高的原因。
// 通过分析进程打开的文件，定位关联的第三方 App、输入法、废纸篓资源和网络连接。
package cursorui

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

const procName = "CursorUIViewService"

// Level 结论的严重程度
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelOK
)

// Finding 一条诊断结论
type Finding struct {
	Level Level
	Text  string
}

// OpenFile lsof 报告的一个打开文件
type OpenFile struct {
	Type string // REG、DIR、IPv4、IPv6、unix 等
	Name string
}

// Report 诊断报告
type Report struct {
	Procs      []sysx.Proc
	Apps       []string   // 涉及的第三方 App 包路径
	SystemApps int        // 涉及的系统自带 App 数量
	Findings   []Finding  // 重点判断
	Suspects   []OpenFile // 可疑明细
	Files      int        // 打开文件总数
}

// Service 诊断操作
type Service struct {
	Runner sysx.Runner
}

// New 使用真实命令执行器
func New() *Service { return &Service{Runner: sysx.ExecRunner{}} }

// Diagnose 查找进程并分析其打开的文件；没有进程时返回空报告
func (s *Service) Diagnose(ctx context.Context) (Report, error) {
	var r Report
	procs, err := sysx.FindProcs(ctx, s.Runner, sysx.ByName(procName))
	if err != nil || len(procs) == 0 {
		return r, err
	}
	r.Procs = procs
	pids := make([]string, len(procs))
	for i, p := range procs {
		pids[i] = strconv.Itoa(p.PID)
	}
	// -F tn 输出机器可读格式，路径含空格也能准确解析；lsof 对部分文件无权限时会返回非零，忽略即可
	out, _ := s.Runner.Run(ctx, sysx.C("lsof", "-nP", "-F", "tn", "-p", strings.Join(pids, ",")))
	files := ParseLsof(out)
	r.Files = len(files)
	r.Apps, r.SystemApps, r.Findings, r.Suspects = Analyze(files)
	return r, nil
}

// ParseLsof 解析 lsof -F tn 的输出：t 开头为类型，n 开头为名称
func ParseLsof(out string) []OpenFile {
	var files []OpenFile
	var cur OpenFile
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'f', 'p':
			cur = OpenFile{}
		case 't':
			cur.Type = line[1:]
		case 'n':
			cur.Name = line[1:]
			files = append(files, cur)
		}
	}
	return files
}

var (
	appPath        = regexp.MustCompile(`^(.*?\.app)/`)
	userAppPath    = regexp.MustCompile(`^(/Applications|/Users/[^/]+/Applications)/`)
	thirdPartyIM   = regexp.MustCompile(`^(/Library|/Users/[^/]+/Library)/Input Methods/`)
	systemLocation = regexp.MustCompile(`^/(System|usr)/`)
)

// Analyze 从打开的文件中归纳涉及的 App、结论与可疑明细。
// /System 下的系统组件只计数，不算作第三方关联。
func Analyze(files []OpenFile) (apps []string, systemApps int, findings []Finding, suspects []OpenFile) {
	appSet, sysSet := map[string]bool{}, map[string]bool{}
	var trash, inputMethods, userApps, network int
	for _, f := range files {
		if m := appPath.FindStringSubmatch(f.Name); m != nil {
			if systemLocation.MatchString(m[1]) {
				sysSet[m[1]] = true
			} else {
				appSet[m[1]] = true
			}
			if userAppPath.MatchString(f.Name) {
				userApps++
			}
		}
		suspect := false
		switch {
		case strings.Contains(f.Name, "/.Trash/"):
			trash++
			suspect = true
		case thirdPartyIM.MatchString(f.Name):
			inputMethods++
			suspect = true
		case f.Type == "IPv4" || f.Type == "IPv6":
			network++
			suspect = true
		}
		if suspect {
			suspects = append(suspects, f)
		}
	}
	for a := range appSet {
		apps = append(apps, a)
	}
	sort.Strings(apps)

	if trash > 0 {
		findings = append(findings, Finding{LevelWarn, "异常：正在读取废纸篓里的资源，可能是已删除 App 的残留"})
	}
	if inputMethods > 0 {
		findings = append(findings, Finding{LevelInfo, "关联：正在读取第三方输入法资源"})
	}
	if userApps > 0 {
		findings = append(findings, Finding{LevelInfo, "关联：正在读取用户安装的 App 资源"})
	}
	if network > 0 {
		findings = append(findings, Finding{LevelInfo, "备注：进程存在网络连接"})
	}
	if len(findings) == 0 {
		findings = append(findings, Finding{LevelOK, "未发现明显的第三方关联，可能是系统文本 UI 服务自身卡住"})
	}
	return apps, len(sysSet), findings, suspects
}

// KillResult 结束进程的结果
type KillResult struct {
	Killed    []int       // 已结束的进程
	Alive     []int       // 未能结束的进程
	Restarted []sysx.Proc // 结束后被系统重新拉起的新进程
}

// Kill 结束进程：先 SIGTERM，超时再 SIGKILL；随后检查系统是否已重新拉起
func (s *Service) Kill(ctx context.Context, procs []sysx.Proc) KillResult {
	pids := sysx.PIDs(procs)
	old := map[int]bool{}
	for _, p := range pids {
		old[p] = true
	}
	var res KillResult
	res.Alive = sysx.Terminate(ctx, s.Runner, pids, 2*time.Second, false)
	dead := map[int]bool{}
	for _, p := range res.Alive {
		dead[p] = true
	}
	for _, p := range pids {
		if !dead[p] {
			res.Killed = append(res.Killed, p)
		}
	}
	if sysx.IsDry(s.Runner) {
		return res
	}
	time.Sleep(time.Second)
	now, _ := sysx.FindProcs(ctx, s.Runner, sysx.ByName(procName))
	for _, p := range now {
		if !old[p.PID] {
			res.Restarted = append(res.Restarted, p)
		}
	}
	return res
}
