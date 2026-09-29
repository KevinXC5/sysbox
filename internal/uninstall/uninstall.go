// Package uninstall 卸载 sysbox：删除程序本身、撤销安装脚本写入的用户 PATH，可选删除配置目录。
package uninstall

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KevinXC5/sysbox/internal/config"
)

// Plan 卸载时要处理的内容
type Plan struct {
	Exe       string // 要删除的可执行文件
	PathDir   string // 要从用户 PATH 移除的目录；为空表示无需处理
	ConfigDir string // 要删除的配置目录；为空表示保留或不存在
}

// NewPlan 根据当前可执行文件生成卸载计划；purge 为 true 时连同配置目录一起删除
func NewPlan(exe string, purge bool) (Plan, error) {
	p := Plan{Exe: exe}
	if dir := filepath.Dir(exe); userPathHas(dir) {
		p.PathDir = dir
	}
	if purge {
		dir, err := config.Dir()
		if err != nil {
			return p, err
		}
		if _, err := os.Stat(dir); err == nil {
			p.ConfigDir = dir
		} else if !errors.Is(err, fs.ErrNotExist) {
			return p, err
		}
	}
	return p, nil
}

// Items 面向用户的操作清单，用于确认前展示
func (p Plan) Items() []string {
	items := []string{"删除程序 " + p.Exe}
	if p.PathDir != "" {
		items = append(items, "从用户 PATH 移除 "+p.PathDir)
	}
	if p.ConfigDir != "" {
		items = append(items, "删除配置目录 "+p.ConfigDir)
	}
	return items
}

// Run 按计划执行卸载。程序本身最后删除，前面的步骤失败时还能用它重试
func (p Plan) Run() error {
	if p.ConfigDir != "" {
		if err := os.RemoveAll(p.ConfigDir); err != nil {
			return fmt.Errorf("删除配置目录失败：%w", err)
		}
	}
	if p.PathDir != "" {
		if err := removeUserPath(p.PathDir); err != nil {
			return fmt.Errorf("从用户 PATH 移除失败：%w", err)
		}
	}
	if err := removeExecutable(p.Exe); err != nil {
		return fmt.Errorf("删除程序失败：%w", err)
	}
	return nil
}

// removeEntry 从 Windows 风格（分号分隔）的 PATH 值中去掉 dir，其余各项保持原样和顺序。
// 比较时不区分大小写、忽略末尾的斜杠；ok 表示是否找到该项。
func removeEntry(value, dir string) (rest string, ok bool) {
	want := strings.TrimRight(dir, `\/`)
	var kept []string
	for _, part := range strings.Split(value, ";") {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(part), `\/`), want) {
			ok = true
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, ";"), ok
}
