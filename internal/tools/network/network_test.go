package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCheckHTTPKeepsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/other")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	report := Check(context.Background(), server.URL, Via{Mode: RouteDirect})
	if !report.OK() || report.Status != "302 Found" {
		t.Fatalf("应保留原始重定向响应：%+v", report)
	}
	last := report.Steps[len(report.Steps)-1]
	if last.Name != "HTTP" || !strings.Contains(last.Detail, "/other") {
		t.Fatalf("HTTP 环节应包含跳转地址：%+v", last)
	}
}

func TestCheckLocatesFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if report := Check(context.Background(), address, Via{}); !report.OK() || report.Steps[len(report.Steps)-1].Name != "TCP" {
		t.Fatalf("主机:端口应只做 TCP 检测：%+v", report)
	}
	listener.Close()
	report := Check(context.Background(), address, Via{})
	if report.OK() || report.Failed != "TCP" || !strings.Contains(report.Steps[len(report.Steps)-1].Detail, "拒绝") {
		t.Fatalf("端口未监听应定位到 TCP：%+v", report)
	}
	report = Check(context.Background(), "http://"+address, Via{Mode: RouteDirect})
	if report.OK() || report.Failed != "连接" {
		t.Fatalf("HTTP 链路应定位到连接环节：%+v", report)
	}
}

func TestCheckUsesGivenProxy(t *testing.T) {
	var seen string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.String()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	report := Check(context.Background(), "http://sysbox.invalid/ping", Via{Mode: RouteProxy, Proxy: u})
	if !report.OK() || seen != "http://sysbox.invalid/ping" || report.Steps[1].Name != "连接代理" {
		t.Fatalf("应经指定代理访问，且本机 DNS 失败不阻断：%+v", report)
	}
}

func TestCheckCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if report := Check(ctx, server.URL, Via{Mode: RouteDirect}); report.OK() {
		t.Fatal("HTTP 请求应响应取消")
	}
}

func TestURLAndProxyRedaction(t *testing.T) {
	for _, target := range []string{"file:///tmp/a", "ftp://example.com", "https://user:secret@example.com"} {
		if _, err := URL(target); err == nil {
			t.Fatalf("应拒绝目标：%s", target)
		}
	}
	if got := RedactProxy("http://user:secret@localhost:7890"); strings.Contains(got, "user") || strings.Contains(got, "secret") {
		t.Fatalf("代理凭据未隐藏：%s", got)
	}
	for _, value := range []string{"user:secret@localhost:7890", "http://user:bad%secret@localhost:7890"} {
		if got := RedactProxy(value); strings.Contains(got, "secret") || strings.Contains(got, "user") {
			t.Fatal("无协议或格式错误的代理也必须隐藏凭据")
		}
	}
}

func TestSystemProxyParsing(t *testing.T) {
	mac := parseScutil("<dictionary> {\n  ExceptionsList : <array> {\n    0 : localhost\n  }\n  HTTPEnable : 1\n  HTTPPort : 7890\n  HTTPProxy : 127.0.0.1\n  HTTPSEnable : 1\n  HTTPSPort : 7890\n  HTTPSProxy : 127.0.0.1\n  SOCKSEnable : 0\n  SOCKSPort : 7891\n  SOCKSProxy : 127.0.0.1\n}")
	if mac.HTTPS != "127.0.0.1:7890" || mac.SOCKS != "" || len(mac.Exceptions) != 1 || mac.URL().String() != "http://127.0.0.1:7890" {
		t.Fatalf("scutil 解析错误：%+v", mac)
	}
	if cmd := ExportCommand(mac, false); !strings.HasPrefix(cmd, "export http_proxy=http://127.0.0.1:7890 https_proxy=") {
		t.Fatalf("终端代理命令错误：%s", cmd)
	}
	win := parseWinInet("    ProxyEnable    REG_DWORD    0x1\r\n    ProxyServer    REG_SZ    http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:7891\r\n")
	if win.HTTP != "127.0.0.1:7890" || win.SOCKS != "127.0.0.1:7891" {
		t.Fatalf("Windows 代理解析错误：%+v", win)
	}
	if cmd := ExportCommand(win, true); !strings.Contains(cmd, "$env:HTTPS_PROXY='http://127.0.0.1:7890'") {
		t.Fatalf("PowerShell 代理命令错误：%s", cmd)
	}
	if parseWinInet("    ProxyEnable    REG_DWORD    0x0\r\n    ProxyServer    REG_SZ    127.0.0.1:7890").Enabled() {
		t.Fatal("关闭的系统代理不应视为开启")
	}
}
