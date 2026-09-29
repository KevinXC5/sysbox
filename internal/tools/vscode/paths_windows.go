//go:build windows

package vscode

import "path/filepath"

// platformOptions Windows：应用数据在 %APPDATA%，扩展与 CLI 在 %USERPROFILE%\.vscode。
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
	return Options{
		Home: home,
		Channels: []Channel{
			{
				Name:      "Code",
				AppRoot:   resolve(filepath.Join(roaming, "Code")),
				ExtRoot:   resolve(filepath.Join(user, ".vscode", "extensions")),
				CLIRoots:  cliRoots(user, ".vscode", ".vscode-server"),
				ProcNames: []string{"Code", "Code Helper"},
				AppLabel:  "Roaming\\Code",
				ExtLabel:  ".vscode\\extensions",
			},
			{
				Name:      "Code - Insiders",
				AppRoot:   resolve(filepath.Join(roaming, "Code - Insiders")),
				ExtRoot:   resolve(filepath.Join(user, ".vscode-insiders", "extensions")),
				CLIRoots:  cliRoots(user, ".vscode-insiders", ".vscode-server-insiders"),
				ProcNames: []string{"Code - Insiders", "Code - Insiders Helper"},
				AppLabel:  "Roaming\\Code - Insiders",
				ExtLabel:  ".vscode-insiders\\extensions",
			},
		},
	}
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
