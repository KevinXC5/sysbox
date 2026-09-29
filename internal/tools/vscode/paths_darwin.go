//go:build darwin

package vscode

import "path/filepath"

// platformOptions macOS：应用数据在 ~/Library/Application Support/<编辑器>，
// 扩展与 CLI 在家目录的 .vscode、.cursor 等目录。
func platformOptions(home string, _ func(string) string) Options {
	support := filepath.Join(home, "Library", "Application Support")
	o := Options{Home: home}
	for _, e := range editors {
		o.Channels = append(o.Channels, Channel{
			Name:      e.name,
			AppRoot:   resolve(filepath.Join(support, e.app)),
			ExtRoot:   resolve(filepath.Join(home, e.dot, "extensions")),
			CLIRoots:  cliRoots(home, e.dot, e.server),
			ProcNames: procNames(e.app),
			AppLabel:  "Application Support/" + e.app,
			ExtLabel:  e.dot + "/extensions",
		})
	}
	return o
}

// procNames 主进程与各类 helper 进程
func procNames(app string) []string {
	h := app + " Helper"
	return []string{app, h, h + " (Plugin)", h + " (Renderer)", h + " (GPU)"}
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
