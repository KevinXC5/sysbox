// Package services 查询与管理系统服务及启动项，命令执行不经过 Shell 拼接。
package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// 来源：第三方可管理；Apple、系统与应用实例默认只读或谨慎操作
const (
	SourceThirdParty = "第三方"
	SourceApple      = "Apple"
	SourceSystem     = "系统"
	SourceApp        = "应用"
)

type Item struct {
	ID, Name, Source, Kind, Scope, State, StartMode string
	Command, Path, Domain, Approval, Mode           string
	PID                                             int
	LogPaths                                        []string
	Startup, Running, Failed, Disabled, Admin       bool
}

type Snapshot struct {
	Items   []Item
	Warning string
}

type Action struct {
	Key, Label, Note, Preview string
	Command                   sysx.Cmd
	Mutates                   bool
}

// Display 确认框与演练模式展示的命令；提权命令展示实际执行的内容
func (a Action) Display() string {
	if a.Preview != "" {
		return a.Preview
	}
	return a.Command.String()
}

type Client struct{ Runner sysx.Runner }

func New() *Client { return &Client{Runner: sysx.ExecRunner{}} }

// List 异常的排在最前，其次是运行中的，同类按名称排序
func (c *Client) List(ctx context.Context, startup bool) (Snapshot, error) {
	s, e := c.list(ctx, startup)
	rank := func(item Item) int {
		switch {
		case item.Failed:
			return 0
		case item.Running:
			return 1
		}
		return 2
	}
	sort.SliceStable(s.Items, func(i, j int) bool {
		a, b := s.Items[i], s.Items[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return s, e
}

func (c *Client) Execute(ctx context.Context, action Action, dryRun bool) (string, error) {
	if dryRun && action.Mutates {
		return "演练模式：将执行 " + action.Display(), nil
	}
	out, err := c.Runner.Run(ctx, action.Command)
	if err != nil {
		return out, fmt.Errorf("请检查服务状态与权限：%w", err)
	}
	if strings.TrimSpace(out) == "" {
		out = action.Label + "完成"
	}
	return out, nil
}
