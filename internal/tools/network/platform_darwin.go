package network

import (
	"context"
	"github.com/KevinXC5/sysbox/internal/sysx"
)

// SystemProxy 只展示系统配置；Go 的 HTTP 诊断不会自动使用 macOS 系统代理。
func SystemProxy(ctx context.Context) (string, error) {
	return (sysx.ExecRunner{}).Run(ctx, sysx.C("scutil", "--proxy"))
}
