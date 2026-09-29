package vscode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// extID 同一扩展、同一目标平台。不同平台的二进制不能互相替代。
type extID struct {
	publisher string
	name      string
	platform  string
}

func (id extID) key() string {
	return strings.ToLower(id.publisher) + "." + strings.ToLower(id.name) + "@" + id.platform
}

// extInstall 磁盘上的一份扩展安装
type extInstall struct {
	dir      string
	id       extID
	version  string
	obsolete bool
}

// indexState 扩展索引的读取结果。ok 为假时整通道跳过扩展清理。
type indexState struct {
	ok         bool
	referenced map[string]string // 目录名（小写）→ 索引里的版本；被引用就不能删
	reason     string
}

// loadIndex 读取扩展目录的 extensions.json，以及各配置档的 extensions.json。
// 默认索引必须在 extRoot，不在应用数据根。任一索引缺失、为空、损坏或配置档无法确认时跳过。
func loadIndex(appRoot, extRoot string) indexState {
	files := []string{filepath.Join(extRoot, "extensions.json")}
	profiles := filepath.Join(appRoot, "User", "profiles")
	entries, err := os.ReadDir(profiles)
	switch {
	case err == nil:
		for _, e := range entries {
			// 符号链接目录可能指向别处，不跟着读，整组跳过
			if e.Type()&os.ModeSymlink != 0 {
				return indexState{reason: "配置档目录是符号链接，无法确认扩展引用"}
			}
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(profiles, e.Name(), "extensions.json")
			st, err := os.Lstat(p)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !st.Mode().IsRegular() {
				return indexState{reason: "配置档 extensions.json 无法读取"}
			}
			files = append(files, p)
		}
	case !os.IsNotExist(err):
		return indexState{reason: "无法读取配置档目录"}
	}
	st := indexState{ok: true, referenced: map[string]string{}}
	for i, f := range files {
		refs, ok, reason := readIndexFile(f)
		if !ok {
			return indexState{reason: reason}
		}
		if i == 0 && !fileExists(f) {
			return indexState{reason: "缺少 extensions.json，无法确认哪些扩展正在使用"}
		}
		for dir, ver := range refs {
			if prev, ok := st.referenced[dir]; ok && prev != ver {
				return indexState{reason: "extensions.json 对同一目录给出了不同版本"}
			}
			st.referenced[dir] = ver
		}
	}
	return st
}

func fileExists(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && !st.IsDir()
}

type indexEntry struct {
	Version          string `json:"version"`
	RelativeLocation string `json:"relativeLocation"`
	Location         struct {
		Path string `json:"path"`
	} `json:"location"`
}

// readIndexFile 解析一份索引。文件不存在时 ok 仍为真（配置档可以没有索引）。
func readIndexFile(path string) (map[string]string, bool, string) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, true, ""
	}
	if err != nil {
		return nil, false, "无法读取 " + filepath.Base(filepath.Dir(path)) + "/extensions.json"
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil, false, "extensions.json 为空"
	}
	var entries []indexEntry
	if err := json.Unmarshal(b, &entries); err != nil || entries == nil {
		return nil, false, "extensions.json 已损坏"
	}
	out := map[string]string{}
	for _, e := range entries {
		dir := e.RelativeLocation
		if dir == "" {
			dir = filepath.Base(filepath.FromSlash(e.Location.Path))
		}
		dir = filepath.Base(dir)
		if dir == "" || dir == "." || strings.Contains(dir, "..") {
			return nil, false, "extensions.json 含无法识别的路径"
		}
		if e.Version == "" {
			return nil, false, "extensions.json 缺少版本"
		}
		key := strings.ToLower(dir)
		if prev, ok := out[key]; ok && prev != e.Version {
			return nil, false, "extensions.json 对同一目录给出了不同版本"
		}
		out[key] = e.Version
	}
	return out, true, ""
}

// readPackage 从扩展目录的 package.json 读取身份。读失败时 ok 为假。
func readPackage(dir string) (id extID, version string, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return id, "", false
	}
	var pkg struct {
		Name      string `json:"name"`
		Publisher string `json:"publisher"`
		Version   string `json:"version"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return id, "", false
	}
	if pkg.Name == "" || pkg.Publisher == "" || pkg.Version == "" {
		return id, "", false
	}
	if _, ok := parseVersion(pkg.Version); !ok {
		return id, "", false
	}
	return extID{publisher: pkg.Publisher, name: pkg.Name, platform: platformOf(filepath.Base(dir))}, pkg.Version, true
}

// platformOf 从目录名末尾取出目标平台。无平台后缀时返回空，表示通用扩展。
// 哈希或提交号不算平台，调用方不会拿它排序。
func platformOf(dirName string) string {
	// 形如 publisher.name-1.2.3-darwin-arm64 或 publisher.name-1.2.3-linux-x64
	known := []string{
		"darwin-arm64", "darwin-x64", "win32-x64", "win32-arm64", "win32-ia32",
		"linux-x64", "linux-arm64", "linux-armhf", "alpine-x64", "alpine-arm64",
		"web",
	}
	lower := strings.ToLower(dirName)
	for _, p := range known {
		if strings.HasSuffix(lower, "-"+p) {
			return p
		}
	}
	return ""
}

// readObsolete 读取 .obsolete。文件不存在视为没有废弃标记；损坏则整份忽略并让调用方跳过扩展清理。
func readObsolete(extRoot string) (map[string]bool, bool) {
	p := filepath.Join(extRoot, ".obsolete")
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return map[string]bool{}, true
	}
	if err != nil {
		return nil, false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]bool{}, true
	}
	var raw map[string]bool
	if json.Unmarshal(b, &raw) != nil {
		return nil, false
	}
	out := map[string]bool{}
	for k, v := range raw {
		if v {
			out[strings.ToLower(filepath.Base(k))] = true
		}
	}
	return out, true
}
