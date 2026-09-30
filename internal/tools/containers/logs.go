package containers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// StreamLogs 固定所选资源、集群与命名空间，持续接收最近日志之后的新输出。
func (c *Client) StreamLogs(ctx context.Context, kind string, item Item, emit func(string)) error {
	plan, err := c.Plan(kind, item, Action{ID: "logs"}, "")
	if err != nil {
		return err
	}
	args := append([]string(nil), plan.Command.Args...)
	for i, arg := range args {
		if arg == "--request-timeout=30s" {
			args[i] = "--request-timeout=0"
		}
	}
	for i, arg := range args {
		if arg == "logs" {
			args = slices.Insert(args, i+1, "--follow")
			break
		}
	}
	command := sysx.C(plan.Command.Name, args...)
	if runner, ok := c.Runner.(sysx.StreamingRunner); ok {
		err = runner.Stream(ctx, command, emit)
	} else {
		// 普通 Runner 仍可用于模拟数据和一次性返回的测试。
		var out string
		out, err = c.Runner.Run(ctx, command)
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if line != "" {
				emit(line)
			}
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("实时日志连接失败，请检查资源状态、网络和权限后按 l 重试：%w", err)
	}
	return err
}
