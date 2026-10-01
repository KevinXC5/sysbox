// Package disk 只读分析目录占用与大文件，不删除文件。
package disk

import (
	"container/heap"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/fsx"
)

type Entry struct {
	Path      string
	Bytes     int64
	Directory bool
	Modified  time.Time
}
type Report struct {
	Path           string
	Entries, Files []Entry
	Bytes          int64
	Skipped        int
}
type Volume struct {
	Name, Path  string
	Total, Free int64
}

func Expand(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
	}
	return filepath.Abs(path)
}

// Scan 一次遍历同时汇总一级目录和前 50 个大文件；不跟随符号链接。
func Scan(ctx context.Context, path string) (Report, error) {
	root, err := Expand(path)
	if err != nil {
		return Report{}, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return Report{}, fmt.Errorf("无法读取目录：%w", err)
	}
	if !info.IsDir() || isLink(info) {
		return Report{}, fmt.Errorf("请输入实际目录，不能扫描符号链接")
	}
	report := Report{Path: root}
	groups := map[string]*Entry{}
	largest := &fileHeap{}
	heap.Init(largest)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			report.Skipped++
			return nil
		}
		if path == root {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			report.Skipped++
			return nil
		}
		if isLink(info) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		name := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		item := groups[name]
		if item == nil {
			item = &Entry{Path: filepath.Join(root, name)}
			groups[name] = item
		}
		if rel == name {
			item.Directory = d.IsDir()
			item.Modified = info.ModTime()
		}
		size := fsx.AllocSize(info)
		item.Bytes += size
		report.Bytes += size
		if info.Mode().IsRegular() {
			entry := Entry{Path: path, Bytes: size, Modified: info.ModTime()}
			if largest.Len() < 50 {
				heap.Push(largest, entry)
			} else if size > (*largest)[0].Bytes {
				heap.Pop(largest)
				heap.Push(largest, entry)
			}
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	for _, entry := range groups {
		report.Entries = append(report.Entries, *entry)
	}
	report.Files = append(report.Files, (*largest)...)
	for _, entries := range [][]Entry{report.Entries, report.Files} {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Bytes == entries[j].Bytes {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].Bytes > entries[j].Bytes
		})
	}
	return report, nil
}

type fileHeap []Entry

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Bytes < h[j].Bytes }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(value any)    { *h = append(*h, value.(Entry)) }
func (h *fileHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

// CleanupTool 按已知目录给出清理入口，具体删除范围仍由清理模块再次扫描确认。
func CleanupTool(path string) string {
	p := "/" + strings.Trim(strings.ToLower(filepath.ToSlash(path)), "/") + "/"
	switch {
	case strings.Contains(p, "jetbrains"):
		return "jetbrains"
	case strings.Contains(p, "vscode") || strings.Contains(p, "code/") || strings.Contains(p, "cursor") || strings.Contains(p, "windsurf") || strings.Contains(p, "trae"):
		return "vscode"
	case strings.Contains(p, ".claude") || strings.Contains(p, ".codex") || strings.Contains(p, "opencode"):
		return "agent"
	case strings.Contains(p, "/node_modules/") || strings.Contains(p, "/target/") || strings.Contains(p, "/.venv/"):
		return "projects"
	}
	if strings.Contains(p, "cache") || strings.Contains(p, ".m2") || strings.Contains(p, ".gradle") || strings.Contains(p, "pkg/mod") {
		return "devcache"
	}
	return ""
}
