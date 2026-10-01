package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
	tea "github.com/charmbracelet/bubbletea"
)

type resourceSaveMsg containers.SaveProgress

func (p *resourcePage) saveImages(directory string) tea.Cmd {
	items := append([]containers.Item(nil), p.actionItems...)
	if len(items) == 0 {
		p.notice = "请先选择镜像"
		return nil
	}
	if directory == "" {
		cwd, err := os.Getwd()
		if err != nil {
			p.err = err
			return nil
		}
		directory = filepath.Join(cwd, "images")
	}
	if directory == "~" || strings.HasPrefix(directory, "~/") || strings.HasPrefix(directory, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			p.err = err
			return nil
		}
		directory = filepath.Join(home, strings.TrimLeft(directory[1:], `/\`))
	}
	p.input.Blur()
	p.mode = resourceList
	p.busy = true
	p.err = nil
	p.notice = fmt.Sprintf("正在打包 %d 个镜像到 %s", len(items), directory)
	p.pullCh = make(chan tea.Msg, 16)
	client, ctx, dryRun, ch := *p.client, p.ctx, p.env.DryRun, p.pullCh
	go func() {
		defer close(ch)
		result, err := client.SaveImages(ctx, items, directory, dryRun, func(progress containers.SaveProgress) {
			select {
			case ch <- resourceSaveMsg(progress):
			case <-ctx.Done():
			}
		})
		title := fmt.Sprintf("镜像打包完成 · %d/%d 个", len(result.Files), len(items))
		if dryRun {
			title = "演练：将生成以下镜像压缩包（未写入文件）"
		}
		output := title + "\n" + strings.Join(result.Files, "\n")
		select {
		case ch <- resourceDoneMsg{output: output, err: err, read: true, title: "镜像打包"}:
		case <-ctx.Done():
		}
	}()
	return p.waitPull()
}

// 保存命令没有额外确认步骤，输出目录可用大写 E 就地修改。
func (p *resourcePage) beginSave(custom bool) tea.Cmd {
	p.action = containers.Action{ID: "save", Label: "打包镜像", Bulk: true}
	p.actionItems = nil
	if len(p.selected) > 0 {
		for _, item := range p.items {
			if p.selected[itemKey(item)] {
				p.actionItems = append(p.actionItems, item)
			}
		}
	} else if item, ok := p.current(); ok {
		p.actionItems = []containers.Item{item}
	}
	if !custom {
		return p.saveImages("")
	}
	cwd, err := os.Getwd()
	if err != nil {
		p.err = err
		return nil
	}
	return p.beginInput("save-directory", "保存目录", filepath.Join(cwd, "images"))
}
