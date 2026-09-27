package sysx

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proc 一个进程的快照
type Proc struct {
	PID     int
	CPU     float64       // 当前 CPU 占用百分比
	CPUTime time.Duration // 累计 CPU 时间
	Elapsed time.Duration // 已运行时长
	RSS     int64         // 常驻内存，字节
	Stat    string
	Path    string // 可执行文件路径
}

// Name 可执行文件名
func (p Proc) Name() string { return filepath.Base(p.Path) }

// BusyRatio 累计 CPU 时间占运行时长的比例，持续接近 1 说明一直占满一个核
func (p Proc) BusyRatio() float64 {
	if p.Elapsed <= 0 {
		return 0
	}
	return float64(p.CPUTime) / float64(p.Elapsed)
}

// psFields ps 输出字段；comm 放最后，路径里的空格才不会打乱列
const psFields = "pid=,%cpu=,time=,etime=,rss=,stat=,comm="

// Procs 列出当前所有进程
func Procs(ctx context.Context, r Runner) ([]Proc, error) {
	out, err := r.Run(ctx, C("ps", "-axo", psFields))
	if err != nil {
		return nil, err
	}
	return ParsePS(out), nil
}

// FindProcs 按过滤条件查找进程，排除 sysbox 自身
func FindProcs(ctx context.Context, r Runner, match func(Proc) bool) ([]Proc, error) {
	all, err := Procs(ctx, r)
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var out []Proc
	for _, p := range all {
		if p.PID != self && match(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// ByName 按可执行文件名精确匹配
func ByName(names ...string) func(Proc) bool {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(p Proc) bool { return set[p.Name()] }
}

// ParsePS 解析 ps -o pid=,%cpu=,time=,etime=,rss=,stat=,comm= 的输出
func ParsePS(out string) []Proc {
	var procs []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(f[1], 64)
		rss, _ := strconv.ParseInt(f[4], 10, 64)
		// comm 可能含空格，取第 7 列起的原始文本
		path := strings.TrimSpace(line[fieldOffset(line, 6):])
		procs = append(procs, Proc{
			PID:     pid,
			CPU:     cpu,
			CPUTime: ParseClock(f[2]),
			Elapsed: ParseClock(f[3]),
			RSS:     rss * 1024,
			Stat:    f[5],
			Path:    path,
		})
	}
	return procs
}

// fieldOffset 返回第 n 个（从 0 开始）空白分隔字段在行内的起始位置
func fieldOffset(line string, n int) int {
	i, field := 0, -1
	inField := false
	for i < len(line) {
		space := line[i] == ' ' || line[i] == '\t'
		if !space && !inField {
			field++
			if field == n {
				return i
			}
		}
		inField = !space
		i++
	}
	return len(line)
}

// ParseClock 解析 ps 的 TIME（分:秒.百分秒）与 ELAPSED（[[天-]时:]分:秒）格式
func ParseClock(s string) time.Duration {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		days, _ = strconv.Atoi(d)
		s = rest
	}
	var frac float64
	if main, f, ok := strings.Cut(s, "."); ok {
		frac, _ = strconv.ParseFloat("0."+f, 64)
		s = main
	}
	total := 0
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(total)*time.Second +
		time.Duration(frac*float64(time.Second))
}

// Terminate 先发 TERM 让进程体面退出，超时后对仍存活的进程发 KILL，返回最终仍存活的进程号。
// 信号经由 Runner 发送：root 为真时以 sudo 执行；演练模式下只记录命令，视为全部结束。
func Terminate(ctx context.Context, r Runner, pids []int, grace time.Duration, root bool) []int {
	if len(pids) == 0 {
		return nil
	}
	kill := func(sig string, targets []int) {
		args := append([]string{"-" + sig}, pidStrings(targets)...)
		c := C("kill", args...)
		c.Sudo = root
		_, _ = r.Run(ctx, c)
	}
	kill("TERM", pids)
	if IsDry(r) {
		return nil
	}
	alive := waitExit(pids, grace)
	if len(alive) > 0 {
		kill("KILL", alive)
		alive = waitExit(alive, time.Second)
	}
	return alive
}

func pidStrings(pids []int) []string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	return s
}

// PIDs 提取进程号
func PIDs(procs []Proc) []int {
	out := make([]int, len(procs))
	for i, p := range procs {
		out[i] = p.PID
	}
	return out
}

// waitExit 轮询等待进程退出，返回超时后仍存活的进程号
func waitExit(pids []int, timeout time.Duration) []int {
	deadline := time.Now().Add(timeout)
	for {
		var alive []int
		for _, pid := range pids {
			if Alive(pid) {
				alive = append(alive, pid)
			}
		}
		if len(alive) == 0 || time.Now().After(deadline) {
			return alive
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Alive 进程是否存在；对无权发信号的进程（EPERM）同样视为存在
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
