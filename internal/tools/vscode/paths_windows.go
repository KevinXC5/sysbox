//go:build windows

package vscode

import "path/filepath"

// platformOptions Windows：应用数据在 %APPDATA%\<编辑器>，扩展与 CLI 在 %USERPROFILE%\.vscode 等目录。
// 环境变量为空时回退到家目录，测试可注入，不读取本机真实目录。
func platformOptions(home string, lookup func(string) string) Options {
	roaming := lookup("APPDATA")
	if roaming == "" {
		roaming = filepath.Join(home, "AppData", "Roaming")
	}
	user := lookup("USERPROFILE")
	if user == "" {
		user = home
	}
	o := Options{Home: home}
	for _, e := range editors {
		o.Channels = append(o.Channels, Channel{
			Name:      e.name,
			AppRoot:   resolve(filepath.Join(roaming, e.app)),
			ExtRoot:   resolve(filepath.Join(user, e.dot, "extensions")),
			CLIRoots:  cliRoots(user, e.dot, e.server),
			ProcNames: []string{e.app, e.app + " Helper"},
			AppLabel:  "Roaming\\" + e.app,
			ExtLabel:  e.dot + "\\extensions",
		})
	}
	return o
}

func cliRoots(home, dot, server string) []string {
	base := filepath.Join(home, dot)
	srv := filepath.Join(home, server)
	return []string{
		resolve(filepath.Join(base, "cli", "servers")),
		resolve(filepath.Join(srv, "bin")),
		resolve(filepath.Join(srv, "cli", "servers")),
	}
}

// forbiddenRoots 盘符根、家目录和 AppData 本身不能删
func forbiddenRoots(o Options) []string {
	roots := []string{string(filepath.Separator), o.Home}
	if vol := filepath.VolumeName(o.Home); vol != "" {
		roots = append(roots, vol+string(filepath.Separator))
	}
	appData := filepath.Join(o.Home, "AppData")
	roots = append(roots,
		appData,
		filepath.Join(appData, "Roaming"),
		filepath.Join(appData, "Local"),
	)
	return roots
}

func configNote() string {
	return "User、Backups 与未识别目录默认不勾选，手动删除后设置、会话和工作区状态无法恢复"
}
