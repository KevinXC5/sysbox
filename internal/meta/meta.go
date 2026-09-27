// Package meta 保存程序的名称、版本和发布仓库等元信息。
package meta

// Version 由构建时通过 -ldflags "-X github.com/KevinXC5/sysbox/internal/meta.Version=..." 注入，
// 本地直接 go build 时为 dev。
var Version = "dev"

const (
	Name = "sysbox"
	// Repo 发布二进制所在的 GitHub 仓库
	Repo = "KevinXC5/sysbox"
)

// IsDev 是否为未注入版本号的本地构建
func IsDev() bool { return Version == "dev" }
