package sysx

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// GUIDomain 当前用户的图形会话域，如 gui/501
func GUIDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// SystemDomain 系统域
const SystemDomain = "system"

// disabledLine 匹配 launchctl print-disabled 的一行："com.foo" => disabled（旧系统为 true）
var disabledLine = regexp.MustCompile(`"([^"]+)"\s*=>\s*(\S+)`)

// DisabledMap 读取某个域下各服务的禁用标记
func DisabledMap(ctx context.Context, r Runner, domain string) (map[string]bool, error) {
	out, err := r.Run(ctx, C("launchctl", "print-disabled", domain))
	if err != nil {
		return nil, err
	}
	return ParseDisabled(out), nil
}

// ParseDisabled 解析 print-disabled 输出，值为 disabled 或 true 表示已禁用
func ParseDisabled(out string) map[string]bool {
	m := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if g := disabledLine.FindStringSubmatch(line); g != nil {
			m[g[1]] = g[2] == "disabled" || g[2] == "true"
		}
	}
	return m
}

// Loaded 服务当前是否已加载到指定域
func Loaded(ctx context.Context, r Runner, domain, label string) bool {
	_, err := r.Run(ctx, C("launchctl", "print", domain+"/"+label))
	return err == nil
}
