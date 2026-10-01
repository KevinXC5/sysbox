package network

import (
	"context"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// SystemProxy 读取系统代理；Go 程序和多数终端工具不会自动使用它
func SystemProxy(ctx context.Context) (ProxySettings, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("scutil", "--proxy"))
	if err != nil {
		return ProxySettings{}, err
	}
	return parseScutil(out), nil
}

func Local(ctx context.Context) (LocalInfo, error) {
	var info LocalInfo
	runner := sysx.ExecRunner{}
	if out, err := runner.Run(ctx, sysx.C("route", "-n", "get", "default")); err == nil {
		for _, line := range strings.Split(out, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			switch key {
			case "gateway":
				info.Gateway = strings.TrimSpace(value)
			case "interface":
				info.GatewayInterface = strings.TrimSpace(value)
			}
		}
	}
	out, err := runner.Run(ctx, sysx.C("scutil", "--dns"))
	if err != nil {
		return info, err
	}
	info.DNS = parseScutilDNS(out)
	return info, nil
}

// parseScutilDNS 只取第一个解析器（系统默认）的 nameserver
func parseScutilDNS(out string) []string {
	var servers []string
	blocks := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "resolver #") {
			blocks++
			if blocks > 1 && len(servers) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "nameserver[") {
			if _, value, ok := strings.Cut(line, " : "); ok {
				servers = append(servers, strings.TrimSpace(value))
			}
		}
	}
	return unique(servers)
}
