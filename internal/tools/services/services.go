// Package services 查询与管理系统服务及启动项，命令执行不经过 Shell 拼接。
package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type Item struct {
	ID, Name, Scope, State, StartMode, Command, Path, Domain string
	PID                                                      int
	LogPaths                                                 []string
	Startup                                                  bool
}
type Snapshot struct {
	Items   []Item
	Warning string
}
type Action struct {
	Key, Label, Note string
	Command          sysx.Cmd
	Mutates          bool
}
type Client struct{ Runner sysx.Runner }

func New() *Client { return &Client{Runner: sysx.ExecRunner{}} }
func (c *Client) List(ctx context.Context, startup bool) (Snapshot, error) {
	s, e := c.list(ctx, startup)
	sort.Slice(s.Items, func(i, j int) bool { return strings.ToLower(s.Items[i].Name) < strings.ToLower(s.Items[j].Name) })
	return s, e
}
func (c *Client) Execute(ctx context.Context, action Action, dryRun bool) (string, error) {
	if dryRun && action.Mutates {
		return "演练模式：将执行 " + action.Command.String(), nil
	}
	out, err := c.Runner.Run(ctx, action.Command)
	if err != nil {
		return out, fmt.Errorf("操作失败，请检查服务状态与权限：%w", err)
	}
	if strings.TrimSpace(out) == "" {
		out = "操作完成"
	}
	return out, nil
}
