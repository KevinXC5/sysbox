package projects

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxDepth      = 7    // 从扫描根往下最多找几层，家目录为根时要多留一层
	activityDepth = 3    // 计算活跃时间时往下看几层源码
	activityLimit = 3000 // 计算活跃时间最多看多少个条目
)

// found 扫描到的一个产物目录
type found struct {
	path    string
	root    string // 所属的项目根目录（配置项）
	project string // 产物所在的项目目录
	rule    rule
	active  time.Time // 项目最近活跃时间
}

// walk 在 root 下查找构建产物，命中后不再深入该产物目录
func walk(root string) []found {
	return walkContext(context.Background(), root, time.Time{})
}

func walkContext(ctx context.Context, root string, now time.Time) []found {
	var out []found
	active := map[string]time.Time{}
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		if ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			// 不跟随符号链接，避免扫出项目根之外的目录
			if !e.IsDir() || e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			name := e.Name()
			if r, ok := match(dir, name); ok {
				if _, ok := active[dir]; !ok {
					active[dir] = activityContext(ctx, dir, now)
				}
				p := filepath.Join(dir, name)
				t := active[dir]
				// 刚装过依赖的旧项目也算活跃
				if fi, err := os.Lstat(p); err == nil && fi.ModTime().After(t) {
					t = fi.ModTime()
				}
				out = append(out, found{path: p, root: root, project: dir, rule: r, active: t})
				continue
			}
			if skipped(dir, name) || depth >= maxDepth {
				continue
			}
			visit(filepath.Join(dir, name), depth+1)
		}
	}
	visit(root, 1)
	return out
}

// activity 项目最近活跃时间：版本库状态文件与浅层源码的最新修改时间。
// 产物目录和隐藏目录不计入，条目太多时提前停止，只求近似。
func activity(project string) time.Time {
	return activityContext(context.Background(), project, time.Time{})
}

func activityContext(ctx context.Context, project string, now time.Time) time.Time {
	var latest time.Time
	bump := func(t time.Time) {
		if t.After(latest) {
			latest = t
		}
	}
	// git 的 index、HEAD 在提交、切换分支、查看状态时都会更新，是很好的活跃信号
	for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD"), "FETCH_HEAD"} {
		if fi, err := os.Lstat(filepath.Join(project, ".git", f)); err == nil {
			bump(fi.ModTime())
		}
	}
	// 一旦已知不足一天前有活动，后续更晚信号仍显示“今天”。
	// 其他日期继续沿用原来的浅层源码扫描，保留准确的天数文案。
	if !now.IsZero() && !latest.IsZero() && now.Sub(latest) < 24*time.Hour {
		return latest
	}
	seen := 0
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		if ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			if seen >= activityLimit {
				return
			}
			name := e.Name()
			if artifactNames[name] || strings.HasPrefix(name, ".") || e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			seen++
			if fi, err := e.Info(); err == nil {
				bump(fi.ModTime())
			}
			if e.IsDir() && depth < activityDepth {
				visit(filepath.Join(dir, name), depth+1)
			}
		}
	}
	visit(project, 1)
	return latest
}
