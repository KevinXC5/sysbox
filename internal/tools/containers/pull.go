package containers

import (
	"context"
	"fmt"
	"strings"
)

type PullProgress struct {
	Name        string
	Done, Total int
}
type PullResult struct {
	Total, Succeeded, Skipped int
	Failures                  []string
}

// PullAll 更新当前 Docker 环境的全部本地标签；失败逐项记录，继续处理其他镜像。
func (c *Client) PullAll(ctx context.Context, dryRun bool, progress func(PullProgress)) (PullResult, error) {
	items, err := c.List(ctx, "images", "")
	if err != nil {
		return PullResult{}, err
	}
	result := PullResult{}
	var images []Item
	seen := map[string]bool{}
	for _, item := range items {
		if strings.Contains(item.Name, "<none>") {
			result.Skipped++
			continue
		}
		if seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		images = append(images, item)
	}
	result.Total = len(images)
	for i, item := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if progress != nil {
			progress(PullProgress{item.Name, i, result.Total})
		}
		plan, err := c.Plan("images", item, Action{ID: "pull", Mutates: true}, item.Name)
		if err == nil {
			_, err = c.Execute(ctx, plan, dryRun)
		}
		if err != nil {
			result.Failures = append(result.Failures, item.Name+"："+err.Error())
		} else {
			result.Succeeded++
		}
	}
	if len(result.Failures) > 0 {
		return result, fmt.Errorf("%d 个镜像拉取失败，请检查网络和仓库权限后重试：\n%s", len(result.Failures), strings.Join(result.Failures, "\n"))
	}
	return result, nil
}
