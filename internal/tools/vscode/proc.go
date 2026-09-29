package vscode

import (
	"context"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Running 检测仍在运行的 VS Code 与 helper。只按可执行文件名匹配，避免扫到无关进程。
func Running(ctx context.Context, r sysx.Runner, names []string) ([]sysx.Proc, error) {
	if len(names) == 0 {
		return nil, nil
	}
	return sysx.FindProcs(ctx, r, sysx.ByName(names...))
}
