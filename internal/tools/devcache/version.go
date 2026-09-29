package devcache

import (
	"strconv"
	"strings"
)

// parseVersion 解析 8.13、9.3.0、8.14-rc-1 这类 Gradle 版本号。
// 返回数字段与是否为预发布；无法解析时 ok 为假，调用方不参与新旧比较。
func parseVersion(s string) (nums []int, pre bool, ok bool) {
	main, suffix, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, false, false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false, false
		}
		nums = append(nums, n)
	}
	return nums, suffix != "", true
}

// olderThan a 严格旧于 b。两边都必须能解析；同号时正式版高于预发布版。
func olderThan(a, b string) bool {
	na, pa, oka := parseVersion(a)
	nb, pb, okb := parseVersion(b)
	if !oka || !okb {
		return false
	}
	for i := range max(len(na), len(nb)) {
		x, y := at(na, i), at(nb, i)
		if x != y {
			return x < y
		}
	}
	return pa && !pb
}

func at(s []int, i int) int {
	if i < len(s) {
		return s[i]
	}
	return 0
}
