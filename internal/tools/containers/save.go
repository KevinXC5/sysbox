package containers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type SaveProgress struct {
	Name        string
	Done, Total int
}
type SaveResult struct {
	Files    []string
	Failures []string
}

// ImageArchiveName 与 SC-Helm 一致：末级镜像名、标签、短 ID、北京时间的镜像创建时间。
func ImageArchiveName(item Item, id string, created time.Time) string {
	name, tag := "untagged", "none"
	if !strings.Contains(item.Name, "<none>") {
		reference := item.Name[strings.LastIndex(item.Name, "/")+1:]
		name = reference
		if i := strings.LastIndex(reference, ":"); i >= 0 {
			name, tag = reference[:i], reference[i+1:]
		} else {
			tag = "latest"
		}
	}
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
				return '-'
			}
			return r
		}, s)
	}
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	stamp := created.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02-15-04-05")
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", clean(name), clean(tag), clean(id), stamp)
}

// SaveImages 逐项流式压缩，保留标签，避免将大镜像读入内存；失败继续处理后续项。
func (c *Client) SaveImages(ctx context.Context, items []Item, directory string, dryRun bool, progress func(SaveProgress)) (SaveResult, error) {
	result := SaveResult{}
	if c.Kubernetes {
		return result, fmt.Errorf("镜像打包仅支持 Docker")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return result, err
	}
	if !dryRun {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return result, fmt.Errorf("无法创建保存目录：%w", err)
		}
	}
	seen := map[string]bool{}
	for i, item := range items {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		if progress != nil {
			progress(SaveProgress{item.Name, i, len(items)})
		}
		path, err := c.saveImage(ctx, item, directory, dryRun)
		if err != nil {
			result.Failures = append(result.Failures, item.Name+"："+err.Error())
		} else {
			result.Files = append(result.Files, path)
		}
	}
	if len(result.Failures) > 0 {
		return result, fmt.Errorf("%d 个镜像打包失败：\n%s", len(result.Failures), strings.Join(result.Failures, "\n"))
	}
	return result, nil
}
func (c *Client) saveImage(ctx context.Context, item Item, directory string, dryRun bool) (string, error) {
	output, err := c.run(ctx, "image", "inspect", item.ID, "--format", "{{json .}}")
	if err != nil {
		return "", err
	}
	var info struct {
		ID      string `json:"Id"`
		Created time.Time
	}
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return "", fmt.Errorf("镜像信息格式有误：%w", err)
	}
	if info.ID == "" || info.Created.IsZero() {
		return "", fmt.Errorf("镜像 ID 或创建时间缺失")
	}
	if len(item.Columns) > 2 && strings.HasPrefix(item.Columns[2], "sha256:") && item.Columns[2] != info.ID {
		return "", fmt.Errorf("镜像标签已变化，请刷新列表后重新选择")
	}
	path := filepath.Join(directory, ImageArchiveName(item, info.ID, info.Created))
	if dryRun {
		return path, nil
	}
	runner, ok := c.Runner.(sysx.WriterRunner)
	if !ok {
		return "", fmt.Errorf("当前执行器不支持镜像流式导出")
	}
	// 独占创建，不覆盖已有归档；出错或取消时删除未完成文件。
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("无法创建压缩包（已有同名文件不会覆盖）：%w", err)
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	compressed := gzip.NewWriter(file)
	if err := runner.RunTo(ctx, c.command("image", "save", item.ID), compressed); err != nil {
		_ = compressed.Close()
		return "", fmt.Errorf("导出镜像失败：%w", err)
	}
	if err := compressed.Close(); err != nil {
		return "", fmt.Errorf("压缩镜像失败：%w", err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	complete = true
	return path, nil
}
