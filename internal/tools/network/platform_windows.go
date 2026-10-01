package network

import (
	"context"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// SystemProxy 读取当前用户的 Internet 代理设置，即系统设置中的代理
func SystemProxy(ctx context.Context) (ProxySettings, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`))
	if err != nil {
		return ProxySettings{}, err
	}
	return parseWinInet(out), nil
}

const localScript = `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false); $r=Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1; if($r){ 'gateway='+$r.NextHop; 'interface='+$r.InterfaceAlias }; Get-DnsClientServerAddress -AddressFamily IPv4,IPv6 | Where-Object { $_.ServerAddresses } | ForEach-Object { $_.ServerAddresses } | ForEach-Object { 'dns='+$_ }`

func Local(ctx context.Context) (LocalInfo, error) {
	var info LocalInfo
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", localScript))
	if err != nil {
		return info, err
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "gateway":
			info.Gateway = value
		case "interface":
			info.GatewayInterface = value
		case "dns":
			info.DNS = append(info.DNS, value)
		}
	}
	info.DNS = unique(info.DNS)
	return info, nil
}
