package agentjunk

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// 单文件安装的 agent 没有版本管理，只能按命名规律找出疑似备份交给人工判断

// backups 目录下形如 name-1.2.3、name.old、name_bak 的疑似备份
func backups(dir, name string) []string {
	if !plainDir(dir) {
		return nil
	}
	// Windows 备份可能是 name.exe.old 或 name-1.2.3.exe，两种都认
	base := strings.TrimSuffix(name, ".exe")
	pattern := regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(base) + `(?:\.exe)?[-_.](?:v?\d+\.\d+\.\d+|old|bak)(?:[-._].*)?(?:\.exe)?$`)
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

// reviewAll 把疑似文件加入待核查。默认不勾选，确认后可删，并按不可恢复警示。
func (s *scanner) reviewAll(paths []string, agent, note string) {
	for _, p := range paths {
		it := cleanup.Item{
			Path: p, Name: filepath.Base(p), Group: agent, Category: CatReview, Note: note,
			Selectable: true, Irreversible: true,
		}
		if isSymlink(p) {
			it.Selectable = false
			it.Irreversible = false
			it.Note = note + "（符号链接，不可选）"
		} else if _, err := identify(p); err != nil {
			it.Category = CatError
			it.Selectable = false
			it.Irreversible = false
			it.Note = err.Error() + "（检查失败，不可选）"
		}
		var latest time.Time
		if fi, err := os.Lstat(p); err == nil {
			it.IsDir = fi.IsDir()
			it.Size, latest, _ = usage(p, true)
			it.Sized = true
		}
		if it.Selectable {
			id, _ := identify(p)
			it.Ref = ref{root: filepath.Dir(p), id: id, manual: true, seen: latest}
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
		same := err == nil && err2 == nil && samePath(target, realBinary)
		// Unix 上入口必须是指向本体的链接。Windows 常把入口复制成普通 exe，
		// EvalSymlinks 会把两边都解成临时目录的真实路径，不能用来判断是不是同一份。
		if goos == "windows" {
			// 复制品和本体不在同一路径，只要入口本身是普通文件就继续找备份
			efi, eerr := os.Lstat(entry)
			if eerr != nil || !efi.Mode().IsRegular() {
				return
			}
		} else if !isSymlink(entry) || !same {
			return
		}
	}
	s.reviewAll(backups(filepath.Dir(binary), filepath.Base(binary)), agent, "疑似旧版备份，未确认是否可删")
}

// inspectBinaries 核查 OpenCode、omp、pi、fx 的安装，只报告不清理
func (s *scanner) inspectBinaries(progress func(string)) {
	progress("核查 OpenCode、omp、pi、fx 的安装")
	s.singleBinary("OpenCode", ".local/bin/"+exe("opencode"), ".opencode/bin/"+exe("opencode"), true)
	s.singleBinary("omp", ".local/bin/"+exe("omp"), ".local/bin/"+exe("omp"), false)
	s.singleBinary("fx", ".local/bin/"+exe("fx"), ".local/bin/"+exe("fx"), false)

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
