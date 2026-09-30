// Package containers 提供 Docker 与 Kubernetes 的资源查询和操作计划。
package containers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type Item struct {
	ID, Name, Namespace, State string
	Columns                    []string
	Containers                 []string
	Fields                     []Field
}

type Action struct {
	ID, Label, Prompt, Default, Note string
	Mutates, Interactive, Bulk       bool
}

type Plan struct {
	Command              sysx.Cmd
	Mutates, Interactive bool
}

// Client 固定当前目标；切换目标仅影响本工具，不改写 Docker 或 kubectl 的全局配置。
type Client struct {
	Kubernetes bool
	Target     string
	Runner     sysx.Runner
}

func New(kubernetes bool) *Client { return &Client{Kubernetes: kubernetes, Runner: sysx.ExecRunner{}} }
func (c *Client) command(args ...string) sysx.Cmd {
	name := "docker"
	var prefix []string
	if c.Kubernetes {
		name = "kubectl"
		prefix = []string{"--context", c.Target, "--request-timeout=30s"}
	} else if c.Target != "" {
		prefix = []string{"--context", c.Target}
	}
	return sysx.C(name, append(prefix, args...)...)
}
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	out, err := c.Runner.Run(ctx, c.command(args...))
	if err != nil {
		return out, fmt.Errorf("执行失败，请检查命令是否安装、目标连接和权限：%w", err)
	}
	return out, nil
}
func (c *Client) Targets(ctx context.Context) ([]string, string, error) {
	if c.Kubernetes {
		out, err := c.Runner.Run(ctx, sysx.C("kubectl", "config", "get-contexts", "-o", "name"))
		if err != nil {
			return nil, "", fmt.Errorf("读取集群失败，请安装 kubectl 并配置 kubeconfig：%w", err)
		}
		current, err := c.Runner.Run(ctx, sysx.C("kubectl", "config", "current-context"))
		targets := strings.Fields(out)
		if len(targets) == 0 {
			return nil, "", errors.New("未找到 Kubernetes 集群，请配置 kubeconfig 后重试")
		}
		if err != nil {
			return targets, "", nil
		}
		return targets, strings.TrimSpace(current), nil
	}
	return c.dockerTargets(ctx)
}
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	out, err := c.run(ctx, "get", "namespaces", "-o", "json")
	if err != nil {
		return nil, err
	}
	items, err := decodeKube(out, "namespaces")
	names := []string{"*"}
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names, err
}
func (c *Client) List(ctx context.Context, kind, namespace string) ([]Item, error) {
	if c.Kubernetes {
		return c.kubeList(ctx, kind, namespace)
	}
	return c.dockerList(ctx, kind)
}
func (c *Client) Execute(ctx context.Context, p Plan, dryRun bool) (string, error) {
	if dryRun && p.Mutates {
		return "演练模式：未执行修改", nil
	}
	if p.Interactive {
		return "", errors.New("交互命令需要在终端中执行")
	}
	// 拉取和节点排空可能较慢，仍然给命令设置上限，避免界面永久等待。
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return c.Runner.Run(ctx, p.Command)
}
func (c *Client) Actions(kind string, item Item) []Action {
	if c.Kubernetes {
		return kubeActions(kind, item)
	}
	return dockerActions(kind, item)
}
func (c *Client) Plan(kind string, item Item, a Action, value string) (Plan, error) {
	value = strings.TrimSpace(value)
	if a.Prompt != "" && value == "" {
		return Plan{}, errors.New("请输入操作参数")
	}
	if strings.ContainsAny(value, "\x00\r\n") || strings.HasPrefix(value, "-") {
		return Plan{}, errors.New("参数不能以 - 开头或包含换行")
	}
	var args []string
	var err error
	if c.Kubernetes {
		args, err = kubeArgs(kind, item, a.ID, value)
	} else {
		args, err = dockerArgs(kind, item, a.ID, value)
	}
	if err != nil {
		return Plan{}, err
	}
	return Plan{c.command(args...), a.Mutates, a.Interactive}, nil
}
