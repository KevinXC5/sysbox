package sysx

import (
	"context"
	"sync"
)

// DryRunner 演练用执行器：只读命令照常执行以反映真实状态，修改类命令只记录不执行并视为成功。
// 工具代码无需区分演练与否，步骤日志即“将要执行的操作”。
type DryRunner struct {
	Real  Runner
	mu    sync.Mutex
	calls []string
}

// NewDryRunner 包装真实执行器
func NewDryRunner(real Runner) *DryRunner { return &DryRunner{Real: real} }

// Run 实现 Runner
func (d *DryRunner) Run(ctx context.Context, c Cmd) (string, error) {
	if readOnly(c) {
		return d.Real.Run(ctx, c)
	}
	d.mu.Lock()
	d.calls = append(d.calls, c.String())
	d.mu.Unlock()
	return "", nil
}

// Calls 被拦截的修改类命令
func (d *DryRunner) Calls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// readOnly 只读命令白名单
func readOnly(c Cmd) bool {
	switch c.Name {
	case "ps", "pgrep", "lsof", "plutil", "sw_vers", "sysctl":
		return true
	case "launchctl":
		return len(c.Args) > 0 && (c.Args[0] == "print" || c.Args[0] == "print-disabled" || c.Args[0] == "list")
	}
	return false
}

// IsDry 执行器是否处于演练模式
func IsDry(r Runner) bool {
	_, ok := r.(*DryRunner)
	return ok
}

// Runner 按演练模式返回合适的执行器
func RunnerFor(dryRun bool) Runner {
	if dryRun {
		return NewDryRunner(ExecRunner{})
	}
	return ExecRunner{}
}
