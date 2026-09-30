package containers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// 仅显式开启时测试真实 Docker，所有修改限于独立的临时标签和容器。
func TestDockerIntegration(t *testing.T) {
	if os.Getenv("SYSBOX_DOCKER_TEST") != "1" {
		t.Skip("设置 SYSBOX_DOCKER_TEST=1 运行 Docker 实测")
	}
	source := os.Getenv("SYSBOX_DOCKER_TEST_IMAGE")
	if source == "" {
		t.Fatal("请用 SYSBOX_DOCKER_TEST_IMAGE 指定已有的本地测试镜像")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := New(false)
	_, target, err := c.Targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Target = target
	name := fmt.Sprintf("sysbox-test-%d", time.Now().UnixNano())
	tag := name + ":test"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = c.run(cleanup, "rm", "-f", name)
		_, _ = c.run(cleanup, "image", "rm", tag)
	})
	perform := func(kind string, item Item, id, value string) string {
		t.Helper()
		a := Action{ID: id, Mutates: id != "logs" && id != "inspect"}
		plan, err := c.Plan(kind, item, a, value)
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.Execute(ctx, plan, false)
		if err != nil {
			t.Fatalf("%s 失败：%v", id, err)
		}
		return out
	}
	perform("images", Item{ID: source}, "tag", tag)
	images, err := c.List(ctx, "images", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, image := range images {
		if image.ID == tag {
			found = true
		}
	}
	if !found {
		t.Fatal("临时镜像标签未列出")
	}
	perform("images", Item{ID: tag}, "run", name)
	lookup := func() Item {
		t.Helper()
		items, err := c.List(ctx, "containers", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Name == name {
				return item
			}
		}
		t.Fatal("临时容器未列出")
		return Item{}
	}
	item := lookup()
	if item.State != "running" {
		t.Fatalf("容器未运行：%+v", item)
	}
	if !strings.Contains(perform("containers", item, "inspect", ""), name) {
		t.Fatal("容器详情未包含名称")
	}
	// 给临时容器写入可识别的日志；现有用户容器不会被操作。
	out, err := c.Runner.Run(ctx, c.command("exec", item.ID, "/bin/sh", "-c", "echo sysbox-docker-test > /proc/1/fd/1"))
	if err != nil {
		t.Fatalf("容器终端命令失败：%s %v", out, err)
	}
	if !strings.Contains(perform("containers", item, "logs", ""), "sysbox-docker-test") {
		t.Fatal("未读取到临时容器日志")
	}
	perform("containers", item, "stop", "")
	if lookup().State == "running" {
		t.Fatal("停止未生效")
	}
	perform("containers", item, "start", "")
	if lookup().State != "running" {
		t.Fatal("启动未生效")
	}
	perform("containers", item, "restart", "")
	if lookup().State != "running" {
		t.Fatal("重启未生效")
	}
	plan, _ := c.Plan("containers", item, Action{ID: "stop", Mutates: true}, "")
	if _, err := c.Execute(ctx, plan, true); err != nil {
		t.Fatal(err)
	}
	if lookup().State != "running" {
		t.Fatal("演练改变了真实容器状态")
	}
	perform("containers", item, "stop", "")
	perform("containers", item, "rm", "")
	perform("images", Item{ID: tag}, "rmi", "")
	// 确认只删除临时标签，源镜像仍然存在。
	if _, err := c.Runner.Run(ctx, sysx.C("docker", append(c.command().Args, "image", "inspect", source)...)); err != nil {
		t.Fatalf("源镜像受到影响：%v", err)
	}
}

func TestDockerPullIntegration(t *testing.T) {
	if os.Getenv("SYSBOX_DOCKER_TEST_PULL") != "1" {
		t.Skip("设置 SYSBOX_DOCKER_TEST_PULL=1 运行镜像拉取实测")
	}
	c := New(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, target, err := c.Targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Target = target
	tag := "hello-world:latest"
	if _, err := c.run(ctx, "image", "inspect", tag); err == nil {
		t.Skip("测试标签已存在，避免更新用户的现有镜像")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = c.run(ctx, "image", "rm", tag)
	})
	plan, err := c.Plan("images", Item{}, Action{ID: "pull", Mutates: true, Prompt: "镜像"}, tag)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(ctx, plan, false); err != nil {
		t.Fatal(err)
	}
	images, err := c.List(ctx, "images", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range images {
		if item.ID == tag {
			found = true
		}
	}
	if !found {
		t.Fatal("拉取成功后镜像未出现在列表")
	}
}
