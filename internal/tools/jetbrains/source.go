package jetbrains

import (
	"context"
	"fmt"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Source JetBrains 缓存清理数据源
type Source struct {
	Opts   Options
	Runner sysx.Runner
}

// NewSource 使用默认路径和真实命令执行器
func NewSource() (*Source, error) {
	o, err := DefaultOptions()
	if err != nil {
		return nil, err
	}
	return &Source{Opts: o, Runner: sysx.ExecRunner{}}, nil
}

var _ cleanup.Source = (*Source)(nil)

func (s *Source) Title() string                  { return "JetBrains 缓存" }
func (s *Source) Categories() []cleanup.Category { return categories }

func (s *Source) Scan(progress func(string)) ([]cleanup.Item, error) {
	progress("读取 " + s.Opts.CacheRoot)
	return Classify(s.Opts)
}

func (s *Source) Roots() []cleanup.Root {
	return []cleanup.Root{
		{Label: "Caches/JetBrains", Path: s.Opts.CacheRoot},
		{Label: "Logs/JetBrains", Path: s.Opts.LogRoot},
		{Label: "Application Support", Path: s.Opts.AppSupport, Untouched: true},
	}
}

// Check IDE 运行时会持续写入缓存，必须先退出
func (s *Source) Check() *cleanup.Notice {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	procs, err := RunningIDEs(ctx, s.Runner)
	if err != nil || len(procs) == 0 {
		return nil
	}
	n := &cleanup.Notice{
		Blocking: true,
		Title:    "检测到 JetBrains IDE 正在运行",
		Lines:    []string{"IDE 运行时会持续写入缓存，请先完全退出后再清理。", ""},
	}
	for _, p := range procs {
		n.Lines = append(n.Lines, fmt.Sprintf("pid %-7d %s", p.PID, p.Path))
	}
	return n
}

func (s *Source) Remove(it cleanup.Item) error { return s.Opts.Remove(it.Path) }

func (s *Source) Notes() []string {
	return []string{
		"下次打开 IDE 会重建索引，前几分钟可能偏慢",
		"Agent 与补全模型已跳过，不需要重新下载",
	}
}

func (s *Source) Tip() string {
	return "下次打开 IDE 会重建索引，Agent 与补全模型仍在本地"
}
