package sysx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// Name 可执行文件名。Windows 进程快照只给出 exe 名，这里原样返回，比较时再规范化。
func (p Proc) Name() string { return filepath.Base(p.Path) }

// exeName 用于进程名比较：Windows 去掉 .exe 并忽略大小写，其余平台保持原样。
// 调用方传入的名字不带后缀（claude、idea），快照里则是 claude.exe。
func exeName(name string) string {
	name = filepath.Base(name)
	if runtime.GOOS != "windows" {
		return name
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	return name
}

// psFields ps 输出字段；comm 放最后，路径里的空格才不会打乱列
const psFields = "pid=,%cpu=,time=,etime=,rss=,stat=,comm="

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
		set[exeName(n)] = true
	}
	return func(p Proc) bool { return set[exeName(p.Name())] }
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
