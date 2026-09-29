package devcache

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// entry 一个可清理的缓存目录
type entry struct {
	path         string
	group        string // 所属工具：Go、npm、Gradle…
	name         string
	cat          int
	note         string
	irreversible bool
	cmd          *sysx.Cmd // 官方清理命令，可用时优先调用；否则直接删除目录
}

// entries 按工具列出本机存在的缓存目录，路径相同的只保留第一条
func (l *locator) entries() []entry {
	var all []entry
	for _, f := range []func() []entry{
		l.goCaches, l.npm, l.pnpm, l.yarn, l.bun, l.pip, l.uv, l.gradle, l.maven, l.cargo, l.homebrew,
	} {
		all = append(all, f()...)
	}
	seen := map[string]bool{}
	var out []entry
	for _, e := range all {
		k := pathKey(e.path)
		if e.path == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// pathKey 去重用的路径键，Windows 忽略大小写
func pathKey(p string) string {
	p = filepath.Clean(p)
	if goos == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func (l *locator) goCaches() []entry {
	// 一次查询两个变量，输出按参数顺序每行一个
	out := l.ask("go", "env", "GOCACHE", "GOMODCACHE")
	gopath := l.envOr("GOPATH", filepath.Join(l.home, "go"))
	build := pick(line(out, 0), l.getenv("GOCACHE"), filepath.Join(l.caches(), "go-build"))
	mod := pick(line(out, 1), l.getenv("GOMODCACHE"), filepath.Join(gopath, "pkg", "mod"))
	var es []entry
	if build != "" {
		es = append(es, entry{path: build, group: "Go", name: "构建缓存", cat: CatClean,
			note: "编译结果缓存，下次构建时重新生成", cmd: l.cmdIf("go", "clean", "-cache")})
	}
	if mod != "" {
		es = append(es, entry{path: mod, group: "Go", name: "模块缓存", cat: CatDownload,
			note: "依赖模块源码，下次构建时重新下载", cmd: l.cmdIf("go", "clean", "-modcache")})
	}
	return es
}

func (l *locator) npm() []entry {
	def := filepath.Join(l.home, ".npm")
	if l.goos == "windows" {
		def = filepath.Join(l.local(), "npm-cache")
	}
	base := l.envOr("npm_config_cache", def)
	var es []entry
	for _, d := range []struct{ dir, name, note string }{
		{"_cacache", "下载缓存", "npm 包下载缓存，安装时按需重新下载"},
		{"_npx", "npx 缓存", "npx 临时安装的命令，下次运行时重新安装"},
		{"_logs", "日志", "npm 运行日志"},
	} {
		if p := pick(filepath.Join(base, d.dir)); p != "" {
			es = append(es, entry{path: p, group: "npm", name: d.name, cat: CatClean, note: d.note})
		}
	}
	return es
}

func (l *locator) pnpm() []entry {
	var asked string
	if l.has("pnpm") {
		asked = line(l.ask("pnpm", "store", "path"), 0)
	}
	def := filepath.Join(l.home, "Library", "pnpm", "store")
	if l.goos == "windows" {
		def = filepath.Join(l.local(), "pnpm", "store")
	}
	p := pick(asked, def, filepath.Join(l.home, ".local", "share", "pnpm", "store"))
	if p == "" {
		return nil
	}
	return []entry{{path: p, group: "pnpm", name: "store", cat: CatDownload,
		note: "项目的 node_modules 以硬链接引用这里的文件，项目仍在时实际释放会少于显示大小；下次安装重新下载"}}
}

func (l *locator) yarn() []entry {
	v1 := filepath.Join(l.caches(), "Yarn")
	if l.goos == "windows" {
		v1 = filepath.Join(l.local(), "Yarn", "Cache")
	}
	var es []entry
	if p := pick(l.getenv("YARN_CACHE_FOLDER"), v1); p != "" {
		es = append(es, entry{path: p, group: "Yarn", name: "v1 缓存", cat: CatClean, note: "Yarn 1 包下载缓存，安装时按需重新下载"})
	}
	if p := pick(filepath.Join(l.home, ".yarn", "berry", "cache")); p != "" {
		es = append(es, entry{path: p, group: "Yarn", name: "全局缓存", cat: CatClean, note: "Yarn 2+ 全局包缓存，安装时按需重新下载"})
	}
	return es
}

func (l *locator) bun() []entry {
	p := pick(l.getenv("BUN_INSTALL_CACHE_DIR"), filepath.Join(l.home, ".bun", "install", "cache"))
	if p == "" {
		return nil
	}
	return []entry{{path: p, group: "Bun", name: "安装缓存", cat: CatClean, note: "Bun 包下载缓存，安装时按需重新下载"}}
}

func (l *locator) pip() []entry {
	def := filepath.Join(l.caches(), "pip")
	if l.goos == "windows" {
		def = filepath.Join(l.local(), "pip", "Cache")
	}
	p := pick(l.getenv("PIP_CACHE_DIR"), def, filepath.Join(l.home, ".cache", "pip"))
	if p == "" {
		return nil
	}
	return []entry{{path: p, group: "pip", name: "下载缓存", cat: CatClean, note: "wheel 与 HTTP 缓存，安装时按需重新下载"}}
}

func (l *locator) uv() []entry {
	def := filepath.Join(l.home, ".cache", "uv")
	if l.goos == "windows" {
		def = filepath.Join(l.local(), "uv", "cache")
	}
	p := pick(l.getenv("UV_CACHE_DIR"), def)
	if p == "" {
		return nil
	}
	// uv cache clean 会等待其他 uv 进程释放缓存锁，比直接删目录安全
	return []entry{{path: p, group: "uv", name: "缓存", cat: CatClean,
		note: "包下载与构建缓存，已创建的虚拟环境不受影响", cmd: l.cmdIf("uv", "cache", "clean")}}
}

func (l *locator) gradle() []entry {
	base := l.envOr("GRADLE_USER_HOME", filepath.Join(l.home, ".gradle"))
	var es []entry
	caches := filepath.Join(base, "caches")
	if p := pick(filepath.Join(caches, "build-cache-1")); p != "" {
		es = append(es, entry{path: p, group: "Gradle", name: "构建缓存", cat: CatClean, note: "本地构建缓存，下次构建时重新生成"})
	}
	if p := pick(filepath.Join(caches, "modules-2")); p != "" {
		es = append(es, entry{path: p, group: "Gradle", name: "依赖缓存", cat: CatDownload, note: "下载的依赖，下次构建时重新下载"})
	}
	// caches/<版本> 与 daemon/<版本> 按 Gradle 版本划分，只保留最高版本
	for _, d := range []struct{ dir, note string }{
		{"caches", "旧版 Gradle 的脚本与生成缓存，该版本再次运行时重新生成"},
		{"daemon", "旧版 Gradle 守护进程的日志与注册信息"},
	} {
		for _, v := range olderDirs(filepath.Join(base, d.dir), func(name string) string { return name }) {
			es = append(es, entry{path: filepath.Join(base, d.dir, v), group: "Gradle", name: d.dir + "/" + v, cat: CatClean, note: d.note})
		}
	}
	// wrapper/dists/gradle-<版本>-bin|all：项目固定了 wrapper 版本，删除后该项目构建时重新下载
	for _, name := range olderDirs(filepath.Join(base, "wrapper", "dists"), wrapperVersion) {
		es = append(es, entry{path: filepath.Join(base, "wrapper", "dists", name), group: "Gradle", name: name, cat: CatDownload,
			note: "旧版 Gradle 发行包，仍在使用该版本的项目构建时会重新下载"})
	}
	return es
}

// wrapperVersion 从 gradle-8.13-bin 取出 8.13
func wrapperVersion(name string) string {
	v, ok := strings.CutPrefix(name, "gradle-")
	if !ok {
		return ""
	}
	for _, s := range []string{"-bin", "-all"} {
		if t, ok := strings.CutSuffix(v, s); ok {
			return t
		}
	}
	return ""
}

// olderDirs 列出 dir 下版本低于最高版本的子目录名。version 从目录名取版本，取不到的目录不参与。
func olderDirs(dir string, version func(string) string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var latest string
	var names []string
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		v := version(e.Name())
		if _, _, ok := parseVersion(v); !ok {
			continue
		}
		names = append(names, e.Name())
		if latest == "" || olderThan(latest, v) {
			latest = v
		}
	}
	var out []string
	for _, n := range names {
		if olderThan(version(n), latest) {
			out = append(out, n)
		}
	}
	return out
}

func (l *locator) maven() []entry {
	p := pick(filepath.Join(l.home, ".m2", "repository"))
	if p == "" {
		return nil
	}
	return []entry{{path: p, group: "Maven", name: "本地仓库", cat: CatDownload, irreversible: true,
		note: "下载的依赖，下次构建时重新下载；mvn install 安装到本地的构件删除后无法重新下载"}}
}

func (l *locator) cargo() []entry {
	base := l.envOr("CARGO_HOME", filepath.Join(l.home, ".cargo"))
	var es []entry
	for _, d := range []struct {
		dir, name, note string
		cat             int
	}{
		{filepath.Join("registry", "src"), "解压的源码", "依赖源码，构建时从本地压缩包重新解压", CatClean},
		{filepath.Join("git", "checkouts"), "git 检出", "git 依赖的检出目录，构建时从本地仓库重新检出", CatClean},
		{filepath.Join("registry", "cache"), "压缩包缓存", "下载的 crate 压缩包，下次构建时重新下载", CatDownload},
		{filepath.Join("git", "db"), "git 仓库", "git 依赖的本地仓库，下次构建时重新克隆", CatDownload},
	} {
		if p := pick(filepath.Join(base, d.dir)); p != "" {
			es = append(es, entry{path: p, group: "Cargo", name: d.name, cat: d.cat, note: d.note})
		}
	}
	return es
}

func (l *locator) homebrew() []entry {
	if l.goos != "darwin" {
		return nil
	}
	p := pick(l.getenv("HOMEBREW_CACHE"), filepath.Join(l.caches(), "Homebrew"))
	if p == "" {
		return nil
	}
	return []entry{{path: p, group: "Homebrew", name: "下载缓存", cat: CatClean, note: "安装包与元数据缓存，安装或升级时按需重新下载"}}
}

// cmdIf 命令可用时返回清理命令，否则为 nil，删除时改为直接删目录
func (l *locator) cmdIf(name string, args ...string) *sysx.Cmd {
	if !l.has(name) {
		return nil
	}
	c := sysx.C(name, args...)
	return &c
}
