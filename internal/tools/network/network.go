// Package network 提供链路检测、代理对比与本机网络信息。
package network

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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

// Preset 常用开发服务，打开页面时一次检测全部
type Preset struct{ Name, Target string }

var Presets = []Preset{
	{"GitHub", "https://github.com"},
	{"GitHub 文件", "https://raw.githubusercontent.com"},
	{"npm", "https://registry.npmjs.org"},
	{"PyPI", "https://pypi.org/simple/"},
	{"Go 模块代理", "https://proxy.golang.org"},
	{"Docker Hub", "https://registry-1.docker.io/v2/"},
	{"Maven 中央仓库", "https://repo.maven.apache.org/maven2/"},
	{"Google", "https://www.google.com"},
	{"百度", "https://www.baidu.com"},
}

// Step 链路中的一个环节
type Step struct {
	Name     string
	OK       bool
	Duration time.Duration
	Detail   string
}

// Report 一次链路检测的结果
type Report struct {
	Target, Address, Route, Status string
	Steps                          []Step
	IPs                            []string
	Total                          time.Duration
	Failed                         string // 失败的环节，成功时为空
	Err                            error
}

func (r Report) OK() bool { return r.Err == nil }

// Route 检测时使用的线路
type Route int

const (
	RouteEnv    Route = iota // 与终端程序一致：读取代理环境变量
	RouteDirect              // 强制直连
	RouteProxy               // 使用指定代理
)

type Via struct {
	Mode  Route
	Proxy *url.URL
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

// parseTarget 识别检测方式：带非 80/443 端口的主机只测 TCP，其余按网址测完整 HTTP 链路
func parseTarget(target string) (*url.URL, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, "", fmt.Errorf("请输入检测目标")
	}
	if !strings.Contains(target, "://") {
		if host, port, err := net.SplitHostPort(target); err == nil && host != "" {
			switch port {
			case "443":
				target = "https://" + target
			case "80":
				target = "http://" + target
			default:
				return nil, net.JoinHostPort(host, port), nil
			}
		}
	}
	u, err := URL(target)
	return u, "", err
}

// Check 依次检测 DNS、连接、TLS 与 HTTP，记录每个环节的耗时并定位失败位置
func Check(ctx context.Context, target string, via Via) (report Report) {
	report = Report{Target: target, Route: "直连"}
	start := time.Now()
	defer func() { report.Total = time.Since(start) }()
	u, tcp, err := parseTarget(target)
	if err != nil {
		report.Err, report.Failed = err, "输入"
		return report
	}
	host := ""
	var proxy *url.URL
	if tcp != "" {
		report.Address = tcp
		host, _, _ = net.SplitHostPort(tcp)
	} else {
		report.Address = u.String()
		host = u.Hostname()
		switch via.Mode {
		case RouteEnv:
			proxy, err = http.ProxyFromEnvironment(&http.Request{URL: u})
			if err != nil {
				report.Err, report.Failed = fmt.Errorf("代理环境变量有误：%w", err), "代理"
				return report
			}
		case RouteProxy:
			proxy = via.Proxy
		}
		if proxy != nil {
			report.Route = RedactProxy(proxy.String())
		}
	}

	// DNS：走代理时由代理解析域名，本机解析失败只做提示
	if ip := net.ParseIP(host); ip != nil {
		report.IPs = []string{ip.String()}
		report.Steps = append(report.Steps, Step{Name: "DNS", OK: true, Detail: "IP 地址，无需解析"})
	} else {
		t := time.Now()
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		step := Step{Name: "DNS", Duration: time.Since(t)}
		if err != nil {
			step.Detail = Explain(err)
			if proxy == nil {
				report.Steps = append(report.Steps, step)
				report.Err, report.Failed = err, "DNS"
				return report
			}
			step.Detail += "；代理会代为解析"
		} else {
			step.OK = true
			for _, a := range addrs {
				report.IPs = append(report.IPs, a.String())
			}
			step.Detail = fmt.Sprintf("%d 个地址", len(addrs))
		}
		report.Steps = append(report.Steps, step)
	}

	if tcp != "" {
		t := time.Now()
		conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", tcp)
		step := Step{Name: "TCP", Duration: time.Since(t)}
		if err != nil {
			step.Detail = Explain(err)
			report.Steps = append(report.Steps, step)
			report.Err, report.Failed = err, "TCP"
			return report
		}
		step.OK, step.Detail = true, "→ "+conn.RemoteAddr().String()
		conn.Close()
		report.Steps = append(report.Steps, step)
		report.Status = "端口可连接"
		return report
	}
	probe(ctx, u, proxy, &report)
	return report
}

func probe(ctx context.Context, u *url.URL, proxy *url.URL, report *Report) {
	var mu sync.Mutex
	var connectStart, tlsStart, firstByte time.Time
	var connectDur, tlsDur time.Duration
	var connectErr, tlsErr error
	var connectAddr string
	connected, tlsDone := false, false
	start := time.Now()
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) { mu.Lock(); connectStart = time.Now(); mu.Unlock() },
		ConnectDone: func(_, addr string, err error) {
			mu.Lock()
			connectDur, connectErr, connectAddr = time.Since(connectStart), err, addr
			connected = err == nil
			mu.Unlock()
		},
		TLSHandshakeStart: func() { mu.Lock(); tlsStart = time.Now(); mu.Unlock() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mu.Lock()
			tlsDur, tlsErr, tlsDone = time.Since(tlsStart), err, true
			mu.Unlock()
		},
		GotFirstResponseByte: func() { mu.Lock(); firstByte = time.Now(); mu.Unlock() },
	}
	// 不跟随重定向，保留原始状态，避免把诊断目标悄悄变成另一个地址
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(*http.Request) (*url.URL, error) { return proxy, nil }
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, u.String(), nil)
	if err != nil {
		report.Err, report.Failed = err, "HTTP"
		return
	}
	request.Header.Set("User-Agent", "sysbox-network-diagnostic")
	response, err := client.Do(request)
	mu.Lock()
	defer mu.Unlock()
	connectName := "连接"
	if proxy != nil {
		connectName = "连接代理"
	}
	connect := Step{Name: connectName, OK: connected, Duration: connectDur}
	if connectAddr != "" {
		connect.Detail = "→ " + connectAddr
	}
	if err != nil {
		report.Err = err
		switch {
		case !connected:
			if connectErr != nil {
				err = connectErr
			}
			connect.Detail = Explain(err)
			report.Steps = append(report.Steps, connect)
			report.Failed = connect.Name
		case tlsDone && tlsErr != nil || u.Scheme == "https" && !tlsDone:
			report.Steps = append(report.Steps, connect, Step{Name: "TLS", Duration: tlsDur, Detail: Explain(err)})
			report.Failed = "TLS"
		default:
			report.Steps = append(report.Steps, connect)
			if tlsDone {
				report.Steps = append(report.Steps, Step{Name: "TLS", OK: true, Duration: tlsDur, Detail: "证书验证通过"})
			}
			report.Steps = append(report.Steps, Step{Name: "HTTP", Detail: Explain(err)})
			report.Failed = "HTTP"
		}
		return
	}
	response.Body.Close()
	report.Steps = append(report.Steps, connect)
	if tlsDone {
		report.Steps = append(report.Steps, Step{Name: "TLS", OK: true, Duration: tlsDur, Detail: "证书验证通过"})
	}
	wait := time.Since(start)
	if !firstByte.IsZero() {
		wait = firstByte.Sub(start)
	}
	detail := response.Status
	if location := response.Header.Get("Location"); location != "" {
		detail += " → " + location
	}
	report.Steps = append(report.Steps, Step{Name: "HTTP", OK: true, Duration: wait, Detail: detail})
	report.Status = response.Status
}

// Explain 把常见网络错误翻译成直观的原因
func Explain(err error) string {
	if err == nil {
		return ""
	}
	var netErr net.Error
	var dnsErr *net.DNSError
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled):
		return "已取消"
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return "域名不存在或无法解析"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout():
		return "超时"
	case strings.Contains(text, "connection refused"):
		return "连接被拒绝（端口未监听）"
	case strings.Contains(text, "connection reset"):
		return "连接被重置"
	case strings.Contains(text, "x509") || strings.Contains(text, "certificate"):
		return "证书校验失败"
	case strings.Contains(text, "no such host"):
		return "域名无法解析"
	case strings.Contains(text, "network is unreachable") || strings.Contains(text, "no route"):
		return "网络不可达"
	case strings.Contains(text, "proxyconnect"):
		return "代理无法连接"
	}
	return err.Error()
}

// EnvProxy 一项代理环境变量
type EnvProxy struct{ Key, Value string }

var envKeys = []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY"}

// EnvProxies 读取终端代理变量，大写优先，值已隐藏凭据；未设置的项值为空
func EnvProxies() []EnvProxy {
	var out []EnvProxy
	for _, key := range envKeys {
		value := os.Getenv(key)
		if value == "" {
			value = os.Getenv(strings.ToLower(key))
		}
		if key != "NO_PROXY" {
			value = RedactProxy(value)
		}
		out = append(out, EnvProxy{key, value})
	}
	return out
}

// EnvProxySet 终端是否会为 HTTP 请求使用代理
func EnvProxySet() bool {
	for _, item := range EnvProxies()[:2] {
		if item.Value != "" {
			return true
		}
	}
	return false
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
	// 代理语法不合法时也不把 @ 前的潜在凭据带回界面
	if index := strings.LastIndex(original, "@"); index >= 0 {
		return "***@" + original[index+1:]
	}
	return original
}

// ProxySettings 系统代理中已开启的各项地址，未开启为空
type ProxySettings struct {
	HTTP, HTTPS, SOCKS, PAC string
	Exceptions              []string
}

func (s ProxySettings) Enabled() bool { return s.HTTP != "" || s.HTTPS != "" || s.SOCKS != "" }

// URL 选出终端最适合使用的系统代理：优先 HTTPS，其次 HTTP，最后 SOCKS
func (s ProxySettings) URL() *url.URL {
	value := ""
	switch {
	case s.HTTPS != "":
		value = "http://" + s.HTTPS
	case s.HTTP != "":
		value = "http://" + s.HTTP
	case s.SOCKS != "":
		value = "socks5://" + s.SOCKS
	}
	if value == "" {
		return nil
	}
	u, _ := url.Parse(value)
	return u
}

// ExportCommand 生成让终端使用系统代理的设置命令
func ExportCommand(s ProxySettings, windows bool) string {
	web := s.HTTPS
	if web == "" {
		web = s.HTTP
	}
	var pairs [][2]string
	if web != "" {
		pairs = append(pairs, [2]string{"http_proxy", "http://" + web}, [2]string{"https_proxy", "http://" + web})
	}
	if s.SOCKS != "" {
		pairs = append(pairs, [2]string{"all_proxy", "socks5://" + s.SOCKS})
	}
	if len(pairs) == 0 {
		return ""
	}
	pairs = append(pairs, [2]string{"no_proxy", "localhost,127.0.0.1,::1,.local"})
	var parts []string
	for _, p := range pairs {
		if windows {
			parts = append(parts, "$env:"+strings.ToUpper(p[0])+"='"+p[1]+"'")
		} else {
			parts = append(parts, p[0]+"="+p[1])
		}
	}
	if windows {
		return strings.Join(parts, "; ")
	}
	return "export " + strings.Join(parts, " ")
}

// parseScutil 解析 scutil --proxy 的字典输出
func parseScutil(out string) ProxySettings {
	values := map[string]string{}
	var exceptions []string
	inList := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExceptionsList") {
			inList = true
			continue
		}
		if inList {
			if line == "}" {
				inList = false
				continue
			}
			if _, value, ok := strings.Cut(line, " : "); ok {
				exceptions = append(exceptions, strings.TrimSpace(value))
			}
			continue
		}
		if key, value, ok := strings.Cut(line, " : "); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	pick := func(prefix string) string {
		if values[prefix+"Enable"] != "1" || values[prefix+"Proxy"] == "" {
			return ""
		}
		return net.JoinHostPort(values[prefix+"Proxy"], values[prefix+"Port"])
	}
	s := ProxySettings{HTTP: pick("HTTP"), HTTPS: pick("HTTPS"), SOCKS: pick("SOCKS"), Exceptions: exceptions}
	if values["ProxyAutoConfigEnable"] == "1" {
		s.PAC = values["ProxyAutoConfigURLString"]
	}
	return s
}

// parseWinInet 解析 reg query 输出的 Internet 设置
func parseWinInet(out string) ProxySettings {
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[1], "REG_") {
			values[fields[0]] = strings.Join(fields[2:], " ")
		}
	}
	var s ProxySettings
	s.PAC = values["AutoConfigURL"]
	if values["ProxyEnable"] != "0x1" {
		return s
	}
	server := values["ProxyServer"]
	if !strings.Contains(server, "=") {
		s.HTTP, s.HTTPS = server, server
	} else {
		for _, part := range strings.Split(server, ";") {
			key, value, _ := strings.Cut(part, "=")
			switch strings.ToLower(key) {
			case "http":
				s.HTTP = value
			case "https":
				s.HTTPS = value
			case "socks":
				s.SOCKS = value
			}
		}
	}
	if bypass := values["ProxyOverride"]; bypass != "" {
		s.Exceptions = strings.Split(bypass, ";")
	}
	return s
}

// Interface 一个已启用、带地址的网卡
type Interface struct {
	Name, MAC string
	Addrs     []string
}

// Interfaces 列出已启用的非回环网卡，过滤掉没有地址的虚拟接口
func Interfaces() ([]Interface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Interface
	for _, item := range list {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := item.Addrs()
		if err != nil {
			continue
		}
		entry := Interface{Name: item.Name, MAC: item.HardwareAddr.String()}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.IsLinkLocalUnicast() {
				continue
			}
			entry.Addrs = append(entry.Addrs, a.String())
		}
		// IPv4 地址排在前面，更便于识别
		sort.SliceStable(entry.Addrs, func(i, j int) bool {
			return !strings.Contains(entry.Addrs[i], ":") && strings.Contains(entry.Addrs[j], ":")
		})
		if len(entry.Addrs) > 0 {
			out = append(out, entry)
		}
	}
	return out, nil
}

// LocalInfo 默认网关与系统 DNS 服务器
type LocalInfo struct {
	Gateway, GatewayInterface string
	DNS                       []string
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// SortInterfaces 默认路由所在的网卡排在最前
func SortInterfaces(list []Interface, primary string) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name == primary && list[j].Name != primary })
}
