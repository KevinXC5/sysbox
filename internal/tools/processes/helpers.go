package processes

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/KevinXC5/sysbox/internal/ui/search"
)

// Filter 文本保留模糊查询，数字精确匹配 PID 或监听端口，避免命令里的数字误命中。
func Filter(items []Process, query string) []Process {
	filtered := make([]Process, 0, len(items))
	for _, p := range items {
		fields := []string{p.Name, strconv.Itoa(p.PID), p.Command, p.Path}
		for _, port := range p.Ports {
			fields = append(fields, port.Local, port.Protocol)
		}
		matched := true
		for _, term := range strings.Fields(strings.ToLower(query)) {
			if !processTermMatches(p, term, fields) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func processTermMatches(p Process, term string, fields []string) bool {
	portOnly, pidOnly := strings.HasPrefix(term, "port:"), strings.HasPrefix(term, "pid:")
	numberText := term
	if portOnly {
		numberText = strings.TrimPrefix(term, "port:")
	}
	if pidOnly {
		numberText = strings.TrimPrefix(term, "pid:")
	}
	digits := numberText != ""
	for _, c := range numberText {
		if c < '0' || c > '9' {
			digits = false
			break
		}
	}
	if !digits {
		if portOnly || pidOnly {
			return false
		}
		_, ok := search.Score(term, fields...)
		return ok
	}
	number, err := strconv.Atoi(numberText)
	if err != nil || number <= 0 {
		return false
	}
	if !portOnly && p.PID == number {
		return true
	}
	if pidOnly || number > 65535 {
		return false
	}
	// 默认端口查询与列表的监听端口列一致，活动连接不作为占用端口的结果。
	for _, port := range ListeningPorts(p) {
		if port.Number == number {
			return true
		}
	}
	return false
}

// Sort 原地排序；端口取最小绑定端口，无端口的进程在两个方向都放最后。
func Sort(items []Process, key string, descending bool) {
	slices.SortStableFunc(items, func(a, b Process) int {
		order := 0
		switch key {
		case "cpu":
			order = cmp.Compare(a.CPU, b.CPU)
		case "memory":
			order = cmp.Compare(a.Memory, b.Memory)
		case "name":
			order = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "port":
			ap, bp := minimumPort(a), minimumPort(b)
			if ap == 0 && bp != 0 {
				return 1
			}
			if bp == 0 && ap != 0 {
				return -1
			}
			order = cmp.Compare(ap, bp)
		default:
			order = cmp.Compare(a.PID, b.PID)
		}
		if descending {
			order = -order
		}
		if order == 0 {
			order = cmp.Compare(a.PID, b.PID)
		}
		return order
	})
}

func minimumPort(p Process) int {
	minimum := 0
	for _, port := range ListeningPorts(p) {
		if port.Number > 0 && (minimum == 0 || port.Number < minimum) {
			minimum = port.Number
		}
	}
	return minimum
}

// ListeningPorts 返回 TCP 监听和 UDP 绑定端口，活动连接保留在详情中。
func ListeningPorts(p Process) []Port {
	ports := make([]Port, 0, len(p.Ports))
	for _, port := range p.Ports {
		state := strings.ToUpper(port.State)
		if state == "LISTEN" || state == "LISTENING" || state == "BOUND" || strings.Contains(strings.ToLower(port.Protocol), "udp") {
			ports = append(ports, port)
		}
	}
	return ports
}

// PortSummary 将监听端口去重后按端口号展示。
func PortSummary(p Process) string {
	numbers := make([]int, 0)
	for _, port := range ListeningPorts(p) {
		if port.Number > 0 && !slices.Contains(numbers, port.Number) {
			numbers = append(numbers, port.Number)
		}
	}
	slices.Sort(numbers)
	values := make([]string, len(numbers))
	for i, number := range numbers {
		values[i] = strconv.Itoa(number)
	}
	if len(values) == 0 {
		return "—"
	}
	return strings.Join(values, ", ")
}

// Identity 用启动时间和程序路径区分 PID 被复用的进程。
func Identity(p Process) string {
	return fmt.Sprintf("%d\x00%s\x00%s", p.PID, p.Started, p.Path)
}

var proxyCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`)

func proxyKey(key string) bool {
	switch strings.ToUpper(key) {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return true
	}
	return false
}

func environmentValue(key, value string, reveal bool) string {
	if reveal {
		return value
	}
	upper := strings.ToUpper(key)
	for _, sensitive := range []string{"TOKEN", "PASSWORD", "SECRET", "KEY", "CREDENTIAL"} {
		if strings.Contains(upper, sensitive) {
			return "***"
		}
	}
	if proxyKey(key) {
		// 遮蔽代理 URL 的整个认证段，包含用户名、口令及编码后的口令。
		value = proxyCredentials.ReplaceAllString(value, "${1}***@")
		if !strings.Contains(value, "://") && strings.Contains(value, "@") {
			value = "***@" + value[strings.LastIndex(value, "@")+1:]
		}
	}
	return value
}

// EnvironmentLines 只允许查询展示中的值，隐藏的凭据不会成为查询侧信道。
func EnvironmentLines(details Details, reveal bool, query string) []string {
	lines := proxyDifferences(details)
	if details.Environment == nil {
		return append(lines, "环境变量：无法读取或未知")
	}
	if len(details.Environment) == 0 {
		return append(lines, "环境变量：无")
	}
	keys := make([]string, 0, len(details.Environment))
	for key := range details.Environment {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if proxyKey(a) != proxyKey(b) {
			if proxyKey(a) {
				return -1
			}
			return 1
		}
		if order := cmp.Compare(strings.ToLower(a), strings.ToLower(b)); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
	matched := 0
	for _, key := range keys {
		value := environmentValue(key, details.Environment[key], reveal)
		if _, ok := search.Score(query, key, value); ok {
			// 环境变量中的控制字符用转义展示，避免覆盖终端内容。
			lines = append(lines, escapeEnvironment(key)+"="+escapeEnvironment(value))
			matched++
		}
	}
	if matched == 0 {
		lines = append(lines, "没有匹配的环境变量")
	}
	return lines
}

func escapeEnvironment(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

func proxyDifferences(details Details) []string {
	if details.Environment == nil {
		return []string{"代理对比：当前进程环境未知"}
	}
	if details.ParentEnvironment == nil {
		return []string{"代理对比：父进程环境无法读取或未知"}
	}
	keys := make([]string, 0)
	for key := range details.Environment {
		if proxyKey(key) {
			keys = append(keys, key)
		}
	}
	for key := range details.ParentEnvironment {
		if proxyKey(key) && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if len(keys) == 0 {
		return []string{"代理对比：父子进程均无代理环境变量"}
	}
	lines := []string{"代理环境与父进程对比："}
	for _, key := range keys {
		value, exists := details.Environment[key]
		parent, parentExists := details.ParentEnvironment[key]
		status := "相同"
		switch {
		case !exists:
			status = "父进程有，当前进程缺失"
		case !parentExists:
			status = "当前进程有，父进程缺失"
		case value != parent:
			status = "值不同"
		}
		lines = append(lines, "  "+key+"："+status)
	}
	return append(lines, "")
}
