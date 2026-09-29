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
	cacheLabel := s.Opts.CacheLabel
	logLabel := s.Opts.LogLabel
	configLabel := s.Opts.ConfigLabel
	if cacheLabel == "" {
		cacheLabel = "Caches/JetBrains"
	}
	if logLabel == "" {
		logLabel = "Logs/JetBrains"
	}
	if configLabel == "" {
		configLabel = "Application Support"
	}
	roots := []cleanup.Root{
		{Label: cacheLabel, Path: s.Opts.CacheRoot},
	}
	// 日志在缓存内部时，完成页只对比缓存根，避免同一棵目录算两遍
	if !s.Opts.LogInsideCache {
		roots = append(roots, cleanup.Root{Label: logLabel, Path: s.Opts.LogRoot})
	}
	roots = append(roots, cleanup.Root{Label: configLabel, Path: s.Opts.AppSupport, Untouched: true})
	return roots
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
		"跳过项默认不删；手动勾选的 Agent、运行时或模型之后需要重新下载",
		configNote(),
	}
}

func (s *Source) Tip() string {
	return "下次打开 IDE 会重建索引，未勾选的跳过项仍在本地"
}
