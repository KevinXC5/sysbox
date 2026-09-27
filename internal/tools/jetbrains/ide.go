package jetbrains

import (
	"context"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// ideComms JetBrains IDE 主进程的可执行文件名。只认进程名，不扫完整命令行，避免误报
var ideComms = []string{
	"idea", "goland", "webstorm", "pycharm", "clion", "phpstorm",
	"rubymine", "datagrip", "rider", "appcode", "studio", "gateway",
}

// RunningIDEs 检测仍在运行的 JetBrains IDE
func RunningIDEs(ctx context.Context, r sysx.Runner) ([]sysx.Proc, error) {
	return sysx.FindProcs(ctx, r, sysx.ByName(ideComms...))
}
