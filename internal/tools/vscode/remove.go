package vscode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// remove 删除前重新核对：路径必须落在某个通道的允许根之下，且分类仍是 want。
// 条目自身是符号链接时只删链接；父目录指向根外时拒绝。
func (o Options) remove(path string, want int) error {
	p := filepath.Clean(path)
	if !o.allowed(p) {
		return fmt.Errorf("拒绝删除越界路径：%s", p)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	real := filepath.Join(parent, filepath.Base(p))
	if !o.allowed(real) {
		return fmt.Errorf("拒绝删除越界路径（解析后）：%s -> %s", p, real)
	}
	if err := o.recheck(real, want); err != nil {
		return err
	}
	return os.RemoveAll(p)
}

// Remove 删除前重新核对路径边界，只接受默认可清理条目。
func (o Options) Remove(path string) error {
	return o.remove(path, CatClean)
}

// RemoveOptional 删除默认不勾选、但允许手动清理的普通条目。
// 重新扫描后它必须仍可勾选，且不再是默认可清理；索引异常和版本不明仍拒绝。
func (o Options) RemoveOptional(path string) error {
	return o.remove(path, -1)
}

// recheck 按当前磁盘重新分类，防止扫描后伪造条目或索引变化把整组删掉。
// want 为 CatClean 时只接受默认可清理；为 -1 时接受任意仍可勾选的非默认可清理条目。
func (o Options) recheck(path string, want int) error {
	// 保留所有相关通道的原有顺序：共享根的首个分类仍优先。
	relevant := o
	relevant.Channels = nil
	for _, ch := range o.Channels {
		if ch.owns(path) {
			relevant.Channels = append(relevant.Channels, ch)
		}
	}
	items, err := Classify(relevant)
	if err != nil {
		return fmt.Errorf("删除前重新扫描失败：%w", err)
	}
	for _, it := range items {
		if sameClean(it.Path, path) {
			if !it.Selectable {
				return fmt.Errorf("条目不可手动删除：%s", path)
			}
			if want == CatClean && it.Category != CatClean {
				return fmt.Errorf("条目分类已变化：%s", path)
			}
			if want != CatClean && it.Category == CatClean {
				return fmt.Errorf("条目已变为默认可清理，请重新扫描：%s", path)
			}
			return nil
		}
	}
	return fmt.Errorf("条目不在本次扫描结果中：%s", path)
}

func sameClean(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// allowed 只允许删除各通道应用数据、扩展目录、CLI 根下的真子路径。
// 根目录本身和家目录一律拒绝，避免勾选子项时把整棵数据目录删掉。
func (o Options) allowed(p string) bool {
	if p == "" || p == o.Home {
		return false
	}
	for _, f := range forbiddenRoots(o) {
		if p == f {
			return false
		}
	}
	for _, ch := range o.Channels {
		if p == ch.AppRoot || p == ch.ExtRoot {
			return false
		}
		for _, root := range ch.CLIRoots {
			if p == root {
				return false
			}
		}
		if within(p, ch.AppRoot) || within(p, ch.ExtRoot) {
			return true
		}
		for _, root := range ch.CLIRoots {
			if within(p, root) {
				return true
			}
		}
	}
	return false
}

func within(p, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}
