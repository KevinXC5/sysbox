package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPStatusAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/other")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	results, err := Diagnose(context.Background(), 2, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Summary != "302 Found" || !strings.Contains(strings.Join(results[0].Lines, "\n"), "Location：/other") {
		t.Fatalf("应保留原始重定向响应：%+v", results)
	}
}
func TestTCPAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	results, err := Diagnose(context.Background(), 1, listener.Addr().String())
	if err != nil || len(results) != 1 {
		t.Fatalf("本地 TCP 检测失败：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Diagnose(ctx, 1, listener.Addr().String()); err == nil {
		t.Fatal("已取消的检测不能成功")
	}
	if _, err := Diagnose(context.Background(), 1, "localhost"); err == nil {
		t.Fatal("必须提供端口")
	}
}
func TestRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := Diagnose(ctx, 2, server.URL); err == nil {
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
	if u, e := URL("example.com/path"); e != nil || u.Scheme != "https" {
		t.Fatal("应支持自动补全 HTTPS")
	}
}
