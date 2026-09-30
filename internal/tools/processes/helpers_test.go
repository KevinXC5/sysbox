package processes

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterMatchesAcrossFields(t *testing.T) {
	items := []Process{
		{PID: 10, Name: "node", Command: "node server.js", Ports: []Port{{Number: 8080, State: "LISTEN"}}},
		{PID: 11, Name: "node", Ports: []Port{{Number: 3000, State: "LISTEN"}}},
		{PID: 12, Name: "java", Ports: []Port{{Number: 8080, State: "LISTEN"}}},
	}
	for _, query := range []string{"node 8080", "10 server", "NODE 8080"} {
		got := Filter(items, query)
		if len(got) != 1 || got[0].PID != 10 {
			t.Fatalf("查询 %q：得到 %v", query, got)
		}
	}
	if got := Filter(items, "node 9999"); len(got) != 0 {
		t.Fatalf("所有查询词必须命中：%v", got)
	}
}

func TestSortListeningPortsMissingLast(t *testing.T) {
	items := []Process{
		{PID: 1, Ports: []Port{{Number: 80, State: "ESTABLISHED"}}},
		{PID: 2, Ports: []Port{{Number: 9000, State: "LISTEN"}, {Number: 3000, State: "LISTEN"}}},
		{PID: 3, Ports: []Port{{Number: 53, Protocol: "udp"}}},
		{PID: 4, Ports: []Port{{Number: 3000, State: "BOUND"}}},
	}
	for _, tc := range []struct {
		descending bool
		want       []int
	}{{false, []int{3, 2, 4, 1}}, {true, []int{2, 4, 3, 1}}} {
		Sort(items, "port", tc.descending)
		got := make([]int, len(items))
		for i, p := range items {
			got[i] = p.PID
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("降序 %v：得到 %v，希望 %v", tc.descending, got, tc.want)
		}
	}
	if got := PortSummary(Process{Ports: []Port{{Number: 80, State: "ESTABLISHED"}, {Number: 8080, State: "LISTEN"}, {Number: 8080, State: "LISTEN"}}}); got != "8080" {
		t.Fatalf("监听端口摘要：%s", got)
	}
}

func TestEnvironmentSecretsMaskedAndNotSearchable(t *testing.T) {
	details := Details{Environment: map[string]string{
		"ACCESS_TOKEN": "hidden-token-value",
		"HTTPS_PROXY":  "http://account:proxy-password@localhost:7890",
		"http_proxy":   "account:other-password@localhost:7890",
		"PATH":         "/usr/bin\n\x1b[31m",
	}, ParentEnvironment: map[string]string{"HTTPS_PROXY": "http://different:secret@localhost:7890"}}
	got := strings.Join(EnvironmentLines(details, false, ""), "\n")
	for _, secret := range []string{"hidden-token-value", "proxy-password", "other-password", "different", "account"} {
		if strings.Contains(got, secret) {
			t.Fatalf("遮蔽结果泄漏 %q：%s", secret, got)
		}
	}
	if strings.Contains(got, "\x1b") || !strings.Contains(got, `\x1b`) {
		t.Fatalf("控制字符应转义：%q", got)
	}
	for _, query := range []string{"hidden-token-value", "proxy-password"} {
		found := strings.Join(EnvironmentLines(details, false, query), "\n")
		if !strings.Contains(found, "没有匹配的环境变量") {
			t.Fatalf("隐藏值不应参与查询：%s", found)
		}
	}
	revealed := strings.Join(EnvironmentLines(details, true, ""), "\n")
	if !strings.Contains(revealed, "hidden-token-value") || !strings.Contains(revealed, "proxy-password") {
		t.Fatalf("展开后应显示原始值：%s", revealed)
	}
	if !strings.Contains(got, "HTTPS_PROXY：值不同") {
		t.Fatalf("应显示父子代理差异：%s", got)
	}
}

func TestEnvironmentUnknownDiffersFromEmpty(t *testing.T) {
	unknown := strings.Join(EnvironmentLines(Details{}, false, ""), "\n")
	empty := strings.Join(EnvironmentLines(Details{Environment: map[string]string{}, ParentEnvironment: map[string]string{}}, false, ""), "\n")
	if !strings.Contains(unknown, "未知") || strings.Contains(empty, "未知") || !strings.Contains(empty, "父子进程均无") {
		t.Fatalf("未知与空环境应区分：%q / %q", unknown, empty)
	}
}

func TestNumericSearchDoesNotFuzzilyMatchPortsOrCommands(t *testing.T) {
	items := []Process{
		{PID: 1, Name: "listener", Ports: []Port{{Number: 5353, Protocol: "UDP", State: "BOUND"}}},
		{PID: 2, Name: "connection", Ports: []Port{{Number: 5353, Protocol: "TCP", State: "ESTABLISHED"}}},
		{PID: 53513, Command: "app --request=5x3x5x3 --value=5353", Ports: []Port{{Number: 53513, State: "LISTEN"}}},
		{PID: 4, Name: "app5353", Path: "/versions/5353/app"},
		{PID: 5353, Name: "exact-pid"},
	}
	for _, tc := range []struct {
		query string
		want  []int
	}{
		{"5353", []int{1, 5353}}, {"port:5353", []int{1}}, {"pid:5353", []int{5353}},
		{"listener 5353", []int{1}}, {"port:53", []int{}}, {"port:invalid", []int{}},
		{"pid:535", []int{}}, {"53513", []int{53513}},
	} {
		got := []int{}
		for _, p := range Filter(items, tc.query) {
			got = append(got, p.PID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("查询 %q：得到 %v，期望 %v", tc.query, got, tc.want)
		}
	}
}
