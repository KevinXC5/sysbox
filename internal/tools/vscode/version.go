package vscode

import (
	"cmp"
	"strconv"
	"strings"
)

// version 语义化版本。正式版高于同号预发布版。
type version struct {
	nums [3]int
	pre  []string
}

// parseVersion 解析 x.y.z[-pre][+build]。日期形版本（如 2026090407）也按三段数字处理。
// 无法解析时 ok 为假，调用方必须跳过，不能拿哈希或修改时间代替。
func parseVersion(s string) (v version, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return v, false
	}
	s, _, _ = strings.Cut(s, "+")
	var pre string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, false
		}
		v.nums[i] = n
	}
	if pre != "" {
		if strings.ContainsAny(pre, "+/") {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
		for _, p := range v.pre {
			if p == "" {
				return v, false
			}
		}
	}
	return v, true
}

// compareVersion 返回 -1、0、1。预发布标识按语义化版本逐段比较。
func compareVersion(a, b version) int {
	for i := range 3 {
		if c := cmp.Compare(a.nums[i], b.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePre(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.pre), len(b.pre))
}

func comparePre(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return cmp.Compare(na, nb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return cmp.Compare(a, b)
}

// olderThan 仅在两边都能解析且 a 严格更旧时为真
func olderThan(a, b string) bool {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	return oka && okb && compareVersion(va, vb) < 0
}
