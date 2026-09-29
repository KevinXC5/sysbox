package vscode

// editor 一个基于 VS Code 的编辑器。它们的应用数据、扩展索引和远程服务端布局一致，
// 只是目录名和进程名不同，扫描与删除规则完全复用。
type editor struct {
	name   string // 通道名，界面分组用
	app    string // 应用数据目录名
	dot    string // 家目录下存放扩展与 CLI 的目录，如 .vscode
	server string // 远程服务端目录，如 .vscode-server
}

// editors 支持的编辑器，顺序即界面分组顺序
var editors = []editor{
	{name: "Code", app: "Code", dot: ".vscode", server: ".vscode-server"},
	{name: "Code - Insiders", app: "Code - Insiders", dot: ".vscode-insiders", server: ".vscode-server-insiders"},
	{name: "Cursor", app: "Cursor", dot: ".cursor", server: ".cursor-server"},
	{name: "Windsurf", app: "Windsurf", dot: ".windsurf", server: ".windsurf-server"},
	{name: "Trae", app: "Trae", dot: ".trae", server: ".trae-server"},
	{name: "Trae CN", app: "Trae CN", dot: ".trae-cn", server: ".trae-cn-server"},
}
