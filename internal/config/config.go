// Package config 读写 sysbox 的用户配置。
// macOS 位于 $XDG_CONFIG_HOME/sysbox/config.json，未设置时为 ~/.config/sysbox/config.json；
// Windows 位于 %APPDATA%\sysbox\config.json，APPDATA 为空时回退到 os.UserConfigDir()。
// 所有字段都可省略，省略时使用各工具的默认值。
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Config 用户配置
type Config struct {
	Theme    string   `json:"theme,omitempty"` // auto / light / dark
	Agent    Agent    `json:"agent"`
	Projects Projects `json:"projects"`
	Claude   Claude   `json:"claude"`
	Update   Update   `json:"update"`
}

// Agent agent 垃圾清理
type Agent struct {
	KeepDays int `json:"keep_days,omitempty"` // 缓存与日志保留天数，默认 30
}

// Projects 项目构建产物清理
type Projects struct {
	Roots    []string `json:"roots,omitempty"`     // 扫描的项目根目录，支持 ~ 开头；为空时自动查找常见目录
	KeepDays int      `json:"keep_days,omitempty"` // 项目多少天未活动算过期，默认 30
}

// Claude Claude Code 更新
type Claude struct {
	Proxy  string `json:"proxy,omitempty"` // 默认 http://127.0.0.1:7890
	Direct bool   `json:"direct,omitempty"`
}

// Update sysbox 自身的升级检查
type Update struct {
	DisableCheck bool `json:"disable_check,omitempty"` // 关闭启动时的新版本检查
}

// Dir 配置目录。macOS 遵循 XDG_CONFIG_HOME，默认 ~/.config/sysbox；
// Windows 使用 %APPDATA%\sysbox，APPDATA 为空时回退到 os.UserConfigDir()。
func Dir() (string, error) {
	base, err := configBase(os.Getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sysbox"), nil
}

// configBase 配置根目录（不含 sysbox）。lookup 注入环境变量，便于测试。
func configBase(lookup func(string) string) (string, error) {
	if runtime.GOOS == "windows" {
		return windowsConfigBase(lookup)
	}
	return unixConfigBase(lookup)
}

// unixConfigBase macOS：XDG_CONFIG_HOME，否则 ~/.config
func unixConfigBase(lookup func(string) string) (string, error) {
	if dir := lookup("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// windowsConfigBase Windows：%APPDATA%，为空时回退到家目录下的 AppData\Roaming。
// os.UserConfigDir 同样只读 APPDATA，不能作为回退。
func windowsConfigBase(lookup func(string) string) (string, error) {
	if dir := lookup("APPDATA"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "AppData", "Roaming"), nil
}

// Path 配置文件路径
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load 读取配置；文件不存在时返回零值配置
func Load() (Config, error) {
	var c Config
	p, err := Path()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// Save 写入配置，先写临时文件再改名，避免写到一半损坏
func Save(c Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
