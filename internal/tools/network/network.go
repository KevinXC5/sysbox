// Package network 提供 DNS、TCP、HTTP 与代理诊断。
package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Result struct {
	Title, Summary string
	Lines          []string
}

func URL(target string) (*url.URL, error) {
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("请输入 HTTP 或 HTTPS 地址")
	}
	if u.User != nil {
		return nil, fmt.Errorf("地址不能包含用户名或密码")
	}
	return u, nil
}

func Diagnose(ctx context.Context, mode int, target string) ([]Result, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("请输入诊断目标")
	}
	switch mode {
	case 0:
		host := target
		if strings.Contains(host, "://") {
			u, e := URL(host)
			if e != nil {
				return nil, e
			}
			host = u.Hostname()
		} else if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		start := time.Now()
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS 查询失败：%w", err)
		}
		var results []Result
		for _, a := range addresses {
			family := "IPv6"
			if a.IP.To4() != nil {
				family = "IPv4"
			}
			results = append(results, Result{a.String(), family, []string{"域名：" + host, "地址：" + a.String(), "类型：" + family, "查询耗时：" + time.Since(start).Round(time.Millisecond).String()}})
		}
		return results, nil
	case 1:
		address := target
		if strings.Contains(target, "://") {
			u, e := URL(target)
			if e != nil {
				return nil, e
			}
			port := u.Port()
			if port == "" {
				port = "443"
				if u.Scheme == "http" {
					port = "80"
				}
			}
			address = net.JoinHostPort(u.Hostname(), port)
		}
		if _, _, e := net.SplitHostPort(address); e != nil {
			return nil, fmt.Errorf("请输入主机:端口，例如 localhost:8080；IPv6 使用 [::1]:8080")
		}
		start := time.Now()
		conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("TCP 连接失败：%w", err)
		}
		defer conn.Close()
		return []Result{{address, "连接成功", []string{"目标：" + address, "本地地址：" + conn.LocalAddr().String(), "远端地址：" + conn.RemoteAddr().String(), "连接耗时：" + time.Since(start).Round(time.Millisecond).String()}}}, nil
	case 2, 3:
		u, err := URL(target)
		if err != nil {
			return nil, err
		}
		results := []Result{}
		if mode == 3 {
			vars := []string{}
			for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
				if value := os.Getenv(key); value != "" {
					vars = append(vars, key+"="+RedactProxy(value))
				}
			}
			sort.Strings(vars)
			if len(vars) == 0 {
				vars = append(vars, "未设置代理环境变量")
			}
			proxy, e := http.ProxyFromEnvironment(&http.Request{URL: u})
			if e != nil {
				return nil, fmt.Errorf("代理配置有误：%w", e)
			}
			effective := "直连"
			if proxy != nil {
				effective = RedactProxy(proxy.String())
			}
			vars = append(vars, "本次 HTTP 请求路径："+effective, "HTTP 检测使用 Go 的 HTTP_PROXY、HTTPS_PROXY、NO_PROXY 规则；ALL_PROXY 不参与该请求。")
			results = append(results, Result{"代理环境", effective, vars})
		}
		result, e := probeHTTP(ctx, u)
		if e != nil && mode == 3 {
			return append(results, Result{"HTTP 连通性", "失败", []string{e.Error()}}), nil
		}
		if e != nil {
			return nil, e
		}
		return append(results, result), nil
	}
	return nil, fmt.Errorf("未知的诊断类型")
}

func RedactProxy(value string) string {
	original := value
	implicitScheme := !strings.Contains(value, "://") && strings.Contains(value, "@")
	if implicitScheme {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err == nil && u.User != nil {
		u.User = url.User("***")
		redacted := u.String()
		if implicitScheme {
			redacted = strings.TrimPrefix(redacted, "http://")
		}
		return redacted
	}
	// 代理语法不合法时也不把 @ 前的潜在凭据带回界面。
	if index := strings.LastIndex(original, "@"); index >= 0 {
		return "***@" + original[index+1:]
	}
	return original
}

func probeHTTP(ctx context.Context, u *url.URL) (Result, error) {
	start := time.Now()
	var mu sync.Mutex
	durations := map[string]time.Duration{}
	starts := map[string]time.Time{}
	begin := func(key string) { mu.Lock(); starts[key] = time.Now(); mu.Unlock() }
	end := func(key string) {
		mu.Lock()
		if t, ok := starts[key]; ok {
			durations[key] = time.Since(t)
		}
		mu.Unlock()
	}
	trace := &httptrace.ClientTrace{DNSStart: func(httptrace.DNSStartInfo) { begin("DNS") }, DNSDone: func(httptrace.DNSDoneInfo) { end("DNS") }, ConnectStart: func(string, string) { begin("TCP") }, ConnectDone: func(string, string, error) { end("TCP") }, TLSHandshakeStart: func() { begin("TLS") }, TLSHandshakeDone: func(tls.ConnectionState, error) { end("TLS") }, GotFirstResponseByte: func() { mu.Lock(); durations["首字节"] = time.Since(start); mu.Unlock() }}
	// 不跟随重定向，保留原始状态与 Location，避免把诊断目标悄悄变成另一个地址。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("User-Agent", "sysbox-network-diagnostic")
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("HTTP 请求失败：%w", err)
	}
	defer response.Body.Close()
	// 限制读取体积，下载大文件时也能快速返回诊断结果。
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	lines := []string{"地址：" + u.String(), "状态：" + response.Status, "协议：" + response.Proto, "总耗时（最多读取 64 KB）：" + time.Since(start).Round(time.Millisecond).String()}
	if readErr != nil {
		lines = append(lines, "读取响应失败："+readErr.Error())
	}
	mu.Lock()
	for _, key := range []string{"DNS", "TCP", "TLS", "首字节"} {
		if d, ok := durations[key]; ok {
			lines = append(lines, key+"："+d.Round(time.Millisecond).String())
		}
	}
	mu.Unlock()
	for _, key := range []string{"Content-Type", "Content-Length", "Server", "Location"} {
		if value := response.Header.Get(key); value != "" {
			lines = append(lines, key+"："+value)
		}
	}
	if response.TLS != nil {
		lines = append(lines, "TLS：证书验证通过")
	}
	return Result{u.Host, response.Status, lines}, nil
}
