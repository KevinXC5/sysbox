package vscode

import (
	"context"
	"fmt"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Source VS Code 清理数据源
type Source struct {
	Opts   Options
	Runner sysx.Runner
}

// NewSource 使用当前平台默认路径
func NewSource() (*Source, error) {
	o, err := DefaultOptions()
	if err != nil {
		return nil, err
	}
	return &Source{Opts: o, Runner: sysx.ExecRunner{}}, nil
}

var _ cleanup.Source = (*Source)(nil)

func (s *Source) Title() string                  { return "VS Code 清理" }
func (s *Source) Categories() []cleanup.Category { return categories }

func (s *Source) Scan(progress func(string)) ([]cleanup.Item, error) {
	if len(s.Opts.Channels) > 0 {
		progress("读取 " + s.Opts.Channels[0].AppRoot)
	}
	return Classify(s.Opts)
}

func (s *Source) Roots() []cleanup.Root {
	var roots []cleanup.Root
	for _, ch := range s.Opts.Channels {
		label := ch.AppLabel
		if label == "" {
			label = ch.Name
		}
		roots = append(roots, cleanup.Root{Label: label, Path: ch.AppRoot})
		if ch.ExtRoot != "" {
			ext := ch.ExtLabel
			if ext == "" {
				ext = ch.Name + " extensions"
			}
			roots = append(roots, cleanup.Root{Label: ext, Path: ch.ExtRoot})
		}
		for i, root := range ch.CLIRoots {
			roots = append(roots, cleanup.Root{Label: fmt.Sprintf("%s 服务端 %d", ch.Name, i+1), Path: root})
		}
	}
	return roots
}

// Check 编辑器或 helper 仍在运行时禁止删除，避免删掉正在使用的缓存与二进制
func (s *Source) Check() *cleanup.Notice {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var names []string
	for _, ch := range s.Opts.Channels {
		names = append(names, ch.ProcNames...)
	}
	procs, err := Running(ctx, s.Runner, names)
	if err != nil {
		return &cleanup.Notice{
			Blocking: true,
			Title:    "无法检查 VS Code 运行状态",
			Lines:    []string{"为避免删除正在使用的文件，请确认进程查询可用后重试。", err.Error()},
		}
	}
	if len(procs) == 0 {
		return nil
	}
	n := &cleanup.Notice{
		Blocking: true,
		Title:    "检测到 VS Code 正在运行",
		Lines:    []string{"编辑器运行时会占用缓存与扩展，请先完全退出后再清理。", ""},
	}
	for _, p := range procs {
		n.Lines = append(n.Lines, fmt.Sprintf("pid %-7d %s", p.PID, p.Path))
	}
	return n
}

func (s *Source) Remove(it cleanup.Item) error {
	// 界面传来的分类只作提示，真正是否可删仍由磁盘上的重新分类决定
	if it.Category == CatSkip {
		return s.Opts.RemoveOptional(it.Path)
	}
	return s.Opts.Remove(it.Path)
}

func (s *Source) Notes() []string {
	return []string{
		"只清理固定白名单中的可重建缓存，未知目录一律保留",
		"扩展只删除同平台已有更高版本、且没有任何配置档索引引用的旧副本",
		"旧 CLI 与远程服务端默认不勾选，手动删除后重新连接需要重新下载",
		configNote(),
	}
}

func (s *Source) Tip() string {
	return "下次打开 VS Code 会重建缓存；设置、用户数据与最新版本始终保留"
}
