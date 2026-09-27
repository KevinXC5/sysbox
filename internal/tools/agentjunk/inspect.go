package agentjunk

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// 单文件安装的 agent 没有版本管理，只能按命名规律找出疑似备份交给人工判断

// backups 目录下形如 name-1.2.3、name.old、name_bak 的疑似备份
func backups(dir, name string) []string {
	if !plainDir(dir) {
		return nil
	}
	pattern := regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(name) + `[-_.](?:v?\d+\.\d+\.\d+|old|bak)(?:[-._].*)?$`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if pattern.MatchString(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// reviewAll 把疑似文件加入待核查分类
func (s *scanner) reviewAll(paths []string, agent, note string) {
	for _, p := range paths {
		it := cleanup.Item{Path: p, Name: filepath.Base(p), Group: agent, Category: CatReview, Note: note}
		if fi, err := os.Lstat(p); err == nil {
			it.IsDir = fi.IsDir()
			it.Size, _, _ = usage(p, true)
			it.Sized = true
		}
		s.items = append(s.items, it)
	}
}

// singleBinary 检查单文件安装：entry 是命令入口，binary 是实际文件；alias 表示入口应为指向 binary 的链接
func (s *scanner) singleBinary(agent, entry, binary string, alias bool) {
	entry, binary = filepath.Join(s.home, entry), filepath.Join(s.home, binary)
	fi, err := os.Lstat(binary)
	if err != nil || !fi.Mode().IsRegular() {
		return
	}
	if alias {
		target, err := filepath.EvalSymlinks(entry)
		realBinary, err2 := filepath.EvalSymlinks(binary)
		if !isSymlink(entry) || err != nil || err2 != nil || target != realBinary {
			return
		}
	}
	s.reviewAll(backups(filepath.Dir(binary), filepath.Base(binary)), agent, "疑似旧版备份，未确认是否可删")
}

// inspectBinaries 核查 OpenCode、omp、pi、fx 的安装，只报告不清理
func (s *scanner) inspectBinaries(progress func(string)) {
	progress("核查 OpenCode、omp、pi、fx 的安装")
	s.singleBinary("OpenCode", ".local/bin/opencode", ".opencode/bin/opencode", true)
	s.singleBinary("omp", ".local/bin/omp", ".local/bin/omp", false)
	s.singleBinary("fx", ".local/bin/fx", ".local/bin/fx", false)

	// omp 的 natives 是配套组件，不是 CLI 旧版；存在多个版本时只提示
	natives := filepath.Join(s.home, ".omp/natives")
	if plainDir(natives) {
		var versions []string
		entries, _ := os.ReadDir(natives)
		for _, e := range entries {
			if e.IsDir() && semverName.MatchString(e.Name()) {
				versions = append(versions, filepath.Join(natives, e.Name()))
			}
		}
		if len(versions) > 1 {
			s.reviewAll(versions, "omp", "存在多个原生组件版本，依赖关系未确认")
		}
	}

	// pi 通过全局 npm 安装：沿真实路径向上找到包目录，再看同级是否有旧包
	if cmd, err := exec.LookPath("pi"); err == nil && isSymlink(cmd) {
		if real, err := filepath.EvalSymlinks(cmd); err == nil {
			for dir := filepath.Dir(real); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
				if filepath.Base(dir) == "pi-coding-agent" && filepath.Base(filepath.Dir(dir)) == "@earendil-works" {
					s.reviewAll(backups(filepath.Dir(dir), "pi-coding-agent"), "pi", "疑似旧包，未确认是否可删")
					break
				}
			}
		}
	}
}
