package agentjunk

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
)

// platformSuffix 版本名末尾的平台三元组，不参与版本比较
var platformSuffix = regexp.MustCompile(`-(?:aarch64|x86_64|arm64|x64)-(?:apple-darwin|pc-windows-msvc)$`)

// version 可比较的版本号：正式版高于同号预发布版，预发布标识按语义化版本规则逐段比较
type version struct {
	nums    [3]int
	release bool
	pre     []string
}

// parseVersion 按匹配规则解析版本名；不匹配时 ok 为假
func parseVersion(name string, pattern *regexp.Regexp) (v version, ok bool) {
	m := pattern.FindStringSubmatchIndex(name)
	if m == nil {
		return v, false
	}
	for i := range 3 {
		v.nums[i], _ = strconv.Atoi(name[m[2+2*i]:m[3+2*i]])
	}
	suffix := ""
	if pattern == semverName {
		// 去掉构建元数据和平台后缀，剩下的才是预发布标识
		suffix, _, _ = strings.Cut(name[m[7]:], "+")
		suffix = platformSuffix.ReplaceAllString(suffix, "")
	}
	pre := strings.TrimPrefix(suffix, "-")
	v.release = pre == ""
	if pre != "" {
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// compareVersion 返回 -1、0、1
func compareVersion(a, b version) int {
	for i := range 3 {
		if c := cmp.Compare(a.nums[i], b.nums[i]); c != 0 {
			return c
		}
	}
	if a.release != b.release {
		if a.release {
			return 1
		}
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePre(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.pre), len(b.pre))
}

// comparePre 数字标识按数值比较，且低于文本标识
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
