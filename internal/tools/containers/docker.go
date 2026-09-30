package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func (c *Client) dockerTargets(ctx context.Context) ([]string, string, error) {
	out, err := c.Runner.Run(ctx, c.command("context", "ls", "--format", "{{.Name}}"))
	if err != nil {
		return nil, "", fmt.Errorf("读取 Docker 环境失败，请安装 Docker 并启动引擎：%w", err)
	}
	current, err := c.Runner.Run(ctx, c.command("context", "show"))
	targets := strings.Fields(out)
	// DOCKER_HOST 比当前 context 优先，保留用户的环境变量连接方式。
	if os.Getenv("DOCKER_HOST") != "" && os.Getenv("DOCKER_CONTEXT") == "" {
		return append([]string{""}, targets...), "", err
	}
	return targets, strings.TrimSpace(current), err
}
func (c *Client) dockerList(ctx context.Context, kind string) ([]Item, error) {
	args := []string{"ps", "-a", "--no-trunc", "--format", "{{json .}}"}
	if kind == "images" {
		args = []string{"image", "ls", "--no-trunc", "--format", "{{json .}}"}
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return decodeDocker(out, kind)
}
func decodeDocker(out, kind string) ([]Item, error) {
	var items []Item
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("Docker 返回数据格式有误：%w", err)
		}
		get := func(key string) string { var s string; _ = json.Unmarshal(row[key], &s); return s }
		item := Item{ID: get("ID"), Name: get("Names"), State: get("State")}
		item.Columns = []string{get("Image"), get("Status"), get("Ports")}
		if kind == "images" {
			item.Name = get("Repository") + ":" + get("Tag")
			item.Columns = []string{get("Size"), get("CreatedSince"), get("ID")}
			// 未打标签的镜像按 ID 删除，有标签时只删除所选标签。
			if get("Repository") != "<none>" && get("Tag") != "<none>" {
				item.ID = item.Name
			}
		}
		item.Fields = []Field{{"名称", item.Name}, {"容器 ID", item.ID}, {"镜像", get("Image")}, {"状态", get("Status")}, {"端口", get("Ports")}, {"创建时间", get("CreatedAt")}, {"挂载", get("Mounts")}, {"网络", get("Networks")}, {"标签信息", get("Labels")}}
		if kind == "images" {
			item.Fields = []Field{{"名称", item.Name}, {"镜像 ID", get("ID")}, {"大小", get("Size")}, {"创建时间", get("CreatedAt")}, {"摘要", get("Digest")}}
		}
		items = append(items, item)
	}
	return items, nil
}
func dockerActions(kind string, item Item) []Action {
	if kind == "images" {
		return []Action{
			{ID: "inspect", Label: "查看镜像详情"},
			{ID: "run", Label: "创建并启动容器", Prompt: "容器名称（后台运行，使用镜像默认命令）", Mutates: true, Note: "使用镜像默认命令；不自动发布端口或挂载目录"},
			{ID: "tag", Label: "添加镜像标签", Prompt: "新标签，例如 myapp:dev", Mutates: true},
			{ID: "rmi", Label: "删除镜像 / 标签", Mutates: true, Bulk: true, Note: "删除所选标签；无标签镜像按 ID 删除，不强制删除使用中的镜像"},
		}
	}
	actions := []Action{{ID: "inspect", Label: "查看容器详情"}, {ID: "logs", Label: "实时日志"}}
	if item.State == "running" {
		actions = append(actions, Action{ID: "exec", Label: "进入容器终端", Prompt: "容器内 Shell", Default: "/bin/sh", Interactive: true, Mutates: true},
			Action{ID: "pause", Label: "暂停容器", Mutates: true, Bulk: true},
			Action{ID: "stop", Label: "停止容器", Mutates: true, Bulk: true}, Action{ID: "restart", Label: "重启容器", Mutates: true, Bulk: true})
	} else if item.State == "paused" {
		actions = append(actions, Action{ID: "unpause", Label: "恢复容器", Mutates: true, Bulk: true})
	} else {
		actions = append(actions, Action{ID: "start", Label: "启动容器", Mutates: true, Bulk: true})
	}
	return append(actions, Action{ID: "rm", Label: "删除已停止的容器", Mutates: true, Bulk: true, Note: "不强制删除；运行中的容器需要先停止。保留卷和镜像"})
}
func dockerArgs(kind string, item Item, action, value string) ([]string, error) {
	switch action {
	case "pull":
		return []string{"pull", value}, nil
	case "inspect":
		if kind == "images" {
			return []string{"image", "inspect", item.ID}, nil
		}
		return []string{"container", "inspect", item.ID}, nil
	case "logs":
		return []string{"logs", "--tail", "200", "--timestamps", item.ID}, nil
	case "exec":
		return []string{"exec", "-it", item.ID, value}, nil
	case "start", "stop", "restart", "pause", "unpause", "rm":
		return []string{action, item.ID}, nil
	case "rmi":
		return []string{"image", "rm", item.ID}, nil
	case "tag":
		return []string{"image", "tag", item.ID, value}, nil
	case "run":
		if strings.ContainsAny(value, " \t/") {
			return nil, fmt.Errorf("容器名称不能包含空白或 /")
		}
		return []string{"run", "-d", "--name", value, item.ID}, nil
	}
	return nil, fmt.Errorf("不支持的 Docker 操作：%s", action)
}
