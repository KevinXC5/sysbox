// Package search 提供终端列表与详情共用的模糊查询。
package search

import (
	"strings"
	"unicode"
)

// Score 多个空格分隔的词全部需要命中；连续匹配优先，也允许按字符顺序跳字匹配。
// 前面的字段权重更高，资源名称通常作为第一个字段。
func Score(query string, fields ...string) (int, bool) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 0, true
	}
	total := 0
	for _, term := range terms {
		best := -1
		for i, field := range fields {
			if score, ok := termScore(term, strings.ToLower(field)); ok {
				score += max(0, 200-i*20)
				best = max(best, score)
			}
		}
		if best < 0 {
			return 0, false
		}
		total += best
	}
	return total, true
}
func termScore(term, value string) (int, bool) {
	if term == value {
		return 2000, true
	}
	if index := strings.Index(value, term); index >= 0 {
		bonus := 0
		if index == 0 {
			bonus = 300
		}
		return 1000 + bonus - min(index, 200), true
	}
	needle := []rune(term)
	matched, first, last, adjacent, boundaries := 0, -1, -1, 0, 0
	previous := rune(' ')
	for index, r := range []rune(value) {
		if r == needle[matched] {
			if first < 0 {
				first = index
			}
			if last == index-1 {
				adjacent++
			}
			if index == 0 || unicode.IsSpace(previous) || strings.ContainsRune("/-_.:", previous) {
				boundaries++
			}
			last = index
			matched++
			if matched == len(needle) {
				return 300 + adjacent*30 + boundaries*40 - min(last-first-len(needle)+1, 200), true
			}
		}
		previous = r
	}
	return 0, false
}
