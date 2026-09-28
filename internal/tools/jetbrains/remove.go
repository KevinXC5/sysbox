package jetbrains

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Remove 安全删除一个条目。
// 字面路径和解析符号链接后的真实路径都必须落在缓存或日志根目录内；
// 条目本身是符号链接时只删链接，不跟随到目标。
func (o Options) Remove(path string) error {
	p := filepath.Clean(path)
	if !o.allowed(p) {
		return fmt.Errorf("拒绝删除越界路径：%s", p)
	}
	// 只解析父目录，条目自身若是链接则保持原样
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	real := filepath.Join(parent, filepath.Base(p))
	if !o.allowed(real) {
		return fmt.Errorf("拒绝删除越界路径（解析后）：%s -> %s", p, real)
	}
	return os.RemoveAll(p)
}

// allowed 判断路径是否严格位于缓存或日志根目录之下。
// 配置目录、用户数据目录和家目录本身一律拒绝。
// 日志在缓存内部时 LogRoot 就是某个产品的 log 子目录，仍落在缓存根之下，允许删除。
func (o Options) allowed(p string) bool {
	for _, forbidden := range forbiddenRoots(o) {
		if p == forbidden {
			return false
		}
	}
	if within(p, o.AppSupport) || p == o.AppSupport {
		return false
	}
	return within(p, o.CacheRoot) || within(p, o.LogRoot)
}

// within 判断 p 是否是 root 的真子路径
func within(p, root string) bool {
	return root != "" && strings.HasPrefix(p, root+string(filepath.Separator))
}
