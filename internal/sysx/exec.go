// Package sysx 封装与 macOS 系统交互的底层操作：执行命令、查询进程、管理 launchd 服务。
// 上层工具只依赖这里的接口，测试时可以替换成假的实现。
package sysx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Cmd 一条待执行的命令
type Cmd struct {
	Name string
	Args []string
	Sudo bool // 以 sudo -n 执行，要求事先已通过 sudo -v 缓存凭据
}

// C 构造普通命令
func C(name string, args ...string) Cmd { return Cmd{Name: name, Args: args} }

// Root 构造需要 root 权限的命令
func Root(name string, args ...string) Cmd { return Cmd{Name: name, Args: args, Sudo: true} }

// String 返回便于展示的命令文本
func (c Cmd) String() string {
	parts := make([]string, 0, len(c.Args)+2)
	if c.Sudo {
		parts = append(parts, "sudo")
	}
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
	name, args := c.Name, c.Args
	if c.Sudo {
		name, args = "sudo", append([]string{"-n", c.Name}, c.Args...)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
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

// Step 一次操作步骤的执行记录，供界面逐条展示
type Step struct {
	Title string // 步骤说明
	Cmd   string // 实际执行的命令，可为空
	Err   error
	Soft  bool // 失败也属正常（例如服务本来就没加载），界面上弱化显示
}

// Recorder 顺序执行命令并记录步骤
type Recorder struct {
	Runner Runner
	Steps  []Step
}

// Do 执行一条命令并记录结果；soft 表示失败可以接受
func (r *Recorder) Do(ctx context.Context, title string, c Cmd, soft bool) error {
	_, err := r.Runner.Run(ctx, c)
	r.Steps = append(r.Steps, Step{Title: title, Cmd: c.String(), Err: err, Soft: soft})
	return err
}

// Note 记录一条不执行命令的说明
func (r *Recorder) Note(title string, err error) {
	r.Steps = append(r.Steps, Step{Title: title, Err: err})
}

// SudoCached 判断 sudo 凭据是否仍在缓存期内，无需再输入密码
func SudoCached(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil
}
