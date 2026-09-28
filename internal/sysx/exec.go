// Package sysx 封装与操作系统交互的底层操作：执行命令、查询进程。
// 上层工具只依赖这里的接口，测试时可以替换成假的实现。
package sysx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Cmd 一条待执行的命令
type Cmd struct {
	Name string
	Args []string
}

// C 构造普通命令
func C(name string, args ...string) Cmd { return Cmd{Name: name, Args: args} }

// String 返回便于展示的命令文本
func (c Cmd) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Name)
	for _, a := range c.Args {
		if strings.ContainsAny(a, " \t\"'") {
			a = fmt.Sprintf("%q", a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// Runner 执行命令并返回标准输出
type Runner interface {
	Run(ctx context.Context, c Cmd) (string, error)
}

// ExecRunner 基于 os/exec 的真实实现
type ExecRunner struct{}

// Run 执行命令；失败时错误信息附带标准错误输出的首行，便于在界面上直接展示原因
func (ExecRunner) Run(ctx context.Context, c Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			err = fmt.Errorf("%w：%s", err, msg)
		}
	}
	return stdout.String(), err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
