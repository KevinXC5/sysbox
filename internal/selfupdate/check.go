package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Pending 列出比 current 新的正式发布，按版本从新到旧排列；current 为 dev 时返回空列表
func (c *Client) Pending(ctx context.Context, current string) ([]Release, error) {
	resp, err := c.request(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=50", c.APIBase, c.Repo), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var all []Release
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, err
	}
	var out []Release
	for _, r := range all {
		if !r.Draft && !r.Prerelease && Newer(r.Tag, current) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Release) int {
		switch {
		case Newer(a.Tag, b.Tag):
			return -1
		case Newer(b.Tag, a.Tag):
			return 1
		}
		return 0
	})
	return out, nil
}

// Notes 把发布说明整理成逐行文本：统一换行符，去掉末尾的“完整变更记录”链接和首尾空行
func Notes(body string) []string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, " \t")
		if strings.HasPrefix(l, "[完整变更记录]") {
			continue
		}
		lines = append(lines, l)
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
