package devcache

import (
	"fmt"
	"path/filepath"
	"strings"
)

// safePath does not trust roots supplied by environment variables or tool
// commands. Unknown custom locations remain visible but cannot be selected.
// Check the resolved target as well to reject escaping ancestor symlinks.
func (l *locator) safePath(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("缓存路径不是绝对路径")
	}
	home, err := filepath.EvalSymlinks(l.home)
	if err != nil {
		return fmt.Errorf("无法解析家目录：%w", err)
	}
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return fmt.Errorf("无法解析缓存路径：%w", err)
	}
	allowed := func(q string) bool {
		q = pathKey(q)
		if q == pathKey(home) || q == pathKey(filepath.VolumeName(q)+string(filepath.Separator)) {
			return false
		}
		shared := []string{filepath.Join(home, "Library", "Caches"), filepath.Join(home, ".cache")}
		if l.goos == "windows" {
			shared = append(shared, filepath.Join(home, "AppData", "Local"))
		}
		for _, root := range shared {
			if strictWithin(q, pathKey(root)) {
				return true
			}
		}
		roots := []string{
			"go/pkg/mod", ".npm/_cacache", ".npm/_npx", ".npm/_logs",
			"Library/pnpm/store", ".local/share/pnpm/store", ".yarn/berry/cache",
			".bun/install/cache", ".gradle/caches", ".gradle/daemon", ".gradle/wrapper/dists",
			".m2/repository", ".cargo/registry/src", ".cargo/registry/cache", ".cargo/git/checkouts", ".cargo/git/db",
		}
		for _, rel := range roots {
			root := pathKey(filepath.Join(home, filepath.FromSlash(rel)))
			if q == root || strictWithin(q, root) {
				return true
			}
		}
		return false
	}
	if !allowed(p) || !allowed(real) {
		return fmt.Errorf("不在已知缓存范围内，请使用工具自身命令清理自定义目录")
	}
	return nil
}

func strictWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
