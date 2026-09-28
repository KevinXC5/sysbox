package claudeupdate

import (
	"fmt"
	"runtime"
)

// platformID 由系统与架构拼出官方发布清单里的平台键。
// 架构使用官方命名：amd64 对应 x64，arm64 保持 arm64。
func platformID(goos, goarch string, musl bool) (string, error) {
	var arch string
	switch goarch {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("不支持的架构：%s", goarch)
	}
	switch goos {
	case "darwin":
		return "darwin-" + arch, nil
	case "linux":
		id := "linux-" + arch
		if musl {
			id += "-musl"
		}
		return id, nil
	case "windows":
		return "win32-" + arch, nil
	default:
		return "", fmt.Errorf("不支持的系统：%s", goos)
	}
}

// Platform 当前平台在官方发布清单中的键。
// macOS 上经 Rosetta 转译的 x64 进程按 arm64 处理；Linux 上检测到 musl 时使用 musl 变体。
func Platform() (string, error) {
	arch := runtime.GOARCH
	if runtime.GOOS == "darwin" && arch == "amd64" && rosettaTranslated() {
		arch = "arm64"
	}
	return platformID(runtime.GOOS, arch, runtime.GOOS == "linux" && muslLibc())
}
