// Package disk 扫描目录占用并缓存目录树，进入子目录、切换视图都不再重复扫描。
package disk

import (
	"container/heap"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KevinXC5/sysbox/internal/fsx"
)

// Node 目录树中的一个目录或文件
type Node struct {
	Name, Path string
	Bytes      int64
	Files      int
	Dir        bool
	Modified   time.Time
	Children   []*Node
	// 文件很多的目录只单独保留较大的文件，其余合并统计
	Rest      int
	RestBytes int64
	parent    *Node
}

// Tree 一次扫描的结果
type Tree struct {
	mu      sync.Mutex
	Root    *Node
	Largest []*Node
	Skipped int
	Partial bool
	Scanned time.Time
}

// Progress 扫描进度，界面定时读取
type Progress struct {
	files, bytes atomic.Int64
	current      atomic.Value
}

func (p *Progress) Snapshot() (files, bytes int64, current string) {
	current, _ = p.current.Load().(string)
	return p.files.Load(), p.bytes.Load(), current
}

type Volume struct {
	Name, Path  string
	Total, Free int64
}

const (
	keepFiles   = 30      // 每个目录单独保留的文件数
	keepBytes   = 1 << 20 // 超过 1 MB 的文件总是单独保留
	largestSize = 200
)

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

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Within 判断 path 是否位于 root 之内（含 root 本身）
func Within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if runtime.GOOS == "windows" {
		rel, err = filepath.Rel(strings.ToLower(root), strings.ToLower(path))
	}
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type scanner struct {
	ctx      context.Context
	tree     *Tree
	progress *Progress
	reuse    *Tree
	largest  fileHeap
}

// Scan 扫描目录并返回目录树；取消时返回已扫描的部分结果并标记 Partial。
// reuse 为此前扫描过的子目录树，遇到相同路径时直接复用，返回上级目录时无需重扫。
func Scan(ctx context.Context, path string, progress *Progress, reuse *Tree) (*Tree, error) {
	root, err := Expand(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("无法读取目录：%w", err)
	}
	if !info.IsDir() || isLink(info) {
		return nil, fmt.Errorf("请输入实际目录，不能扫描符号链接")
	}
	if progress == nil {
		progress = &Progress{}
	}
	tree := &Tree{Root: &Node{Name: filepath.Base(root), Path: root, Dir: true, Modified: info.ModTime(), Bytes: fsx.AllocSize(info)}}
	s := &scanner{ctx: ctx, tree: tree, progress: progress, reuse: reuse}
	if reuse != nil && reuse.Root != nil && Within(root, reuse.Root.Path) {
		for _, n := range reuse.Largest {
			heap.Push(&s.largest, n)
		}
		tree.Skipped += reuse.Skipped
		tree.Partial = reuse.Partial
	} else {
		s.reuse = nil
	}
	s.scan(tree.Root)
	tree.Largest = make([]*Node, len(s.largest))
	copy(tree.Largest, s.largest)
	sortNodes(tree.Largest)
	tree.Scanned = time.Now()
	if ctx.Err() != nil {
		tree.Partial = true
		return tree, ctx.Err()
	}
	return tree, nil
}

func (s *scanner) push(n *Node) {
	if s.largest.Len() < largestSize {
		heap.Push(&s.largest, n)
	} else if n.Bytes > s.largest[0].Bytes {
		heap.Pop(&s.largest)
		heap.Push(&s.largest, n)
	}
}

func (s *scanner) scan(dir *Node) {
	s.progress.current.Store(dir.Path)
	entries, err := os.ReadDir(dir.Path)
	if err != nil {
		s.tree.Skipped++
		return
	}
	var files []*Node
	for _, entry := range entries {
		if s.ctx.Err() != nil {
			break
		}
		path := filepath.Join(dir.Path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			s.tree.Skipped++
			continue
		}
		if isLink(info) {
			continue
		}
		size := fsx.AllocSize(info)
		if entry.IsDir() {
			var child *Node
			if s.reuse != nil && samePath(path, s.reuse.Root.Path) {
				child = s.reuse.Root
				s.progress.files.Add(int64(child.Files))
				s.progress.bytes.Add(child.Bytes)
			} else {
				child = &Node{Name: entry.Name(), Path: path, Dir: true, Modified: info.ModTime(), Bytes: size}
				s.scan(child)
			}
			child.parent = dir
			dir.Children = append(dir.Children, child)
			dir.Bytes += child.Bytes
			dir.Files += child.Files
			continue
		}
		file := &Node{Name: entry.Name(), Path: path, Bytes: size, Files: 1, Modified: info.ModTime(), parent: dir}
		files = append(files, file)
		dir.Bytes += size
		dir.Files++
		s.progress.files.Add(1)
		s.progress.bytes.Add(size)
		if info.Mode().IsRegular() {
			s.push(file)
		}
	}
	sortNodes(files)
	for i, f := range files {
		if i < keepFiles || f.Bytes >= keepBytes {
			dir.Children = append(dir.Children, f)
		} else {
			dir.Rest++
			dir.RestBytes += f.Bytes
		}
	}
	sortNodes(dir.Children)
}

func sortNodes(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Bytes == nodes[j].Bytes {
			return nodes[i].Path < nodes[j].Path
		}
		return nodes[i].Bytes > nodes[j].Bytes
	})
}

// Find 在已扫描的目录树中定位路径，不在树内时返回 nil
func (t *Tree) Find(path string) *Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.find(path)
}

func (t *Tree) find(path string) *Node {
	if t == nil || t.Root == nil || !Within(t.Root.Path, path) {
		return nil
	}
	node := t.Root
	rel, _ := filepath.Rel(node.Path, path)
	if rel == "." {
		return node
	}
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		var next *Node
		for _, child := range node.Children {
			if samePath(child.Name, name) {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		node = next
	}
	return node
}

// Remove 删除文件后同步更新目录树与各级目录的占用，无需重新扫描
func (t *Tree) Remove(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	parent := t.find(filepath.Dir(path))
	if parent == nil {
		return
	}
	var bytes int64
	files := 0
	removed := false
	for i, child := range parent.Children {
		if samePath(child.Path, path) {
			bytes, files = child.Bytes, max(1, child.Files)
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		// 合并统计的小文件只出现在大文件排行中
		for _, n := range t.Largest {
			if samePath(n.Path, path) {
				bytes, files = n.Bytes, 1
				parent.Rest = max(0, parent.Rest-1)
				parent.RestBytes = max(0, parent.RestBytes-bytes)
				break
			}
		}
	}
	for n := parent; n != nil; n = n.parent {
		n.Bytes -= bytes
		n.Files -= files
	}
	kept := t.Largest[:0]
	for _, n := range t.Largest {
		if !Within(path, n.Path) {
			kept = append(kept, n)
		}
	}
	t.Largest = kept
}

// LargestUnder 返回位于 path 之内的大文件
func (t *Tree) LargestUnder(path string) []*Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*Node
	for _, n := range t.Largest {
		if Within(path, n.Path) {
			out = append(out, n)
		}
	}
	return out
}

// Trashable 只允许处理家目录内的普通内容，家目录本身与系统资料目录不提供删除
func Trashable(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, protected := range []string{home, filepath.Join(home, "Library"), filepath.Join(home, ".Trash"), filepath.Join(home, "AppData")} {
		if samePath(path, protected) {
			return fmt.Errorf("系统目录不能移到废纸篓")
		}
	}
	if !Within(home, path) || Within(filepath.Join(home, ".Trash"), path) {
		return fmt.Errorf("只能处理家目录内的文件")
	}
	return nil
}

// CleanupTool 按已知目录给出清理入口，具体删除范围仍由清理模块再次扫描确认
func CleanupTool(path string) string {
	p := "/" + strings.Trim(strings.ToLower(filepath.ToSlash(path)), "/") + "/"
	has := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(p, part) {
				return true
			}
		}
		return false
	}
	switch {
	case has("/jetbrains/"):
		return "jetbrains"
	case has("/.vscode/", "/.cursor/", "/.windsurf/", "/.trae/", "/application support/code/", "/application support/cursor/", "/appdata/roaming/code/", "/appdata/roaming/cursor/"):
		return "vscode"
	case has("/.claude/", "/.codex/", "/.opencode/", "/opencode/"):
		return "agent"
	case has("/node_modules/", "/.venv/"):
		return "projects"
	case has("/.m2/", "/.gradle/", "/.npm/", "/.pnpm-store/", "/.cargo/registry/", "/go/pkg/mod/", "/go-build/", "/library/caches/pip/", "/library/caches/homebrew/", "/.cache/uv/", "/appdata/local/npm-cache/"):
		return "devcache"
	}
	return ""
}

type fileHeap []*Node

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Bytes < h[j].Bytes }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(value any)    { *h = append(*h, value.(*Node)) }
func (h *fileHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}
