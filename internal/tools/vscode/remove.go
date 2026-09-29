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

// Remove 删除前重新核对路径边界和当前分类。
func (o Options) Remove(path string) error {
	return o.remove(path, CatClean)
}

// RemoveOptional 删除默认不勾选、但允许手动清理的旧 CLI / 服务端。
func (o Options) RemoveOptional(path string) error {
	return o.remove(path, CatSkip)
}

// recheck 按当前磁盘重新分类，防止扫描后伪造条目或索引变化把配置删掉。
// 期望分类来自调用方：缓存必须仍是可清理，服务端旧版本必须仍是默认可选手动项。
func (o Options) recheck(path string, want int) error {
	items, err := Classify(o)
	if err != nil {
		return fmt.Errorf("删除前重新扫描失败：%w", err)
	}
	for _, it := range items {
		if sameClean(it.Path, path) {
			if it.Category != want || !it.Selectable {
				return fmt.Errorf("条目分类已变化：%s", path)
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
// 根目录本身、User、Backups 以及家目录一律拒绝。
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
		if blockedUserPath(p, ch) {
			return false
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

// blockedUserPath User 与 Backups 整树拒绝，避免白名单被绕过
func blockedUserPath(p string, ch Channel) bool {
	for _, name := range []string{"User", "Backups"} {
		root := filepath.Join(ch.AppRoot, name)
		if p == root || within(p, root) {
			return true
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
