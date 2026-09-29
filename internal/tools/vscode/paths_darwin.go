//go:build darwin

package vscode

import "path/filepath"

// platformOptions macOS：稳定版与 Insiders 的应用数据在 ~/Library/Application Support，
// 扩展与 CLI 在家目录的 .vscode / .vscode-insiders。
func platformOptions(home string, _ func(string) string) Options {
	support := filepath.Join(home, "Library", "Application Support")
	return Options{
		Home: home,
		Channels: []Channel{
			{
				Name:      "Code",
				AppRoot:   resolve(filepath.Join(support, "Code")),
				ExtRoot:   resolve(filepath.Join(home, ".vscode", "extensions")),
				CLIRoots:  cliRoots(home, ".vscode", ".vscode-server"),
				ProcNames: []string{"Code", "Code Helper", "Code Helper (Plugin)", "Code Helper (Renderer)", "Code Helper (GPU)"},
				AppLabel:  "Application Support/Code",
				ExtLabel:  ".vscode/extensions",
			},
			{
				Name:      "Code - Insiders",
				AppRoot:   resolve(filepath.Join(support, "Code - Insiders")),
				ExtRoot:   resolve(filepath.Join(home, ".vscode-insiders", "extensions")),
				CLIRoots:  cliRoots(home, ".vscode-insiders", ".vscode-server-insiders"),
				ProcNames: []string{"Code - Insiders", "Code - Insiders Helper", "Code - Insiders Helper (Plugin)", "Code - Insiders Helper (Renderer)", "Code - Insiders Helper (GPU)"},
				AppLabel:  "Application Support/Code - Insiders",
				ExtLabel:  ".vscode-insiders/extensions",
			},
		},
	}
}

// cliRoots 本机 CLI 与远程 server 的安装根。目录不存在时扫描阶段跳过。
func cliRoots(home, dot, server string) []string {
	base := filepath.Join(home, dot)
	srv := filepath.Join(home, server)
	return []string{
		resolve(filepath.Join(base, "cli", "servers")),
		resolve(filepath.Join(srv, "bin")),
		resolve(filepath.Join(srv, "cli", "servers")),
	}
}

// forbiddenRoots 家目录、Library 和应用数据根本身不能删
func forbiddenRoots(o Options) []string {
	return []string{
		"/",
		o.Home,
		filepath.Join(o.Home, "Library"),
		filepath.Join(o.Home, "Library", "Application Support"),
	}
}

// configNote 面向用户的保护说明
func configNote() string {
	return "User、Backups 与未识别目录默认不勾选，手动删除后设置、会话和工作区状态无法恢复"
}
