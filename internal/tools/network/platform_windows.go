package network

import (
	"context"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// SystemProxy 展示 WinHTTP 配置；Go 的 HTTP 诊断仍以环境变量为准。
func SystemProxy(ctx context.Context) (string, error) {
	return (sysx.ExecRunner{}).Run(ctx, sysx.C("netsh", "winhttp", "show", "proxy"))
}
