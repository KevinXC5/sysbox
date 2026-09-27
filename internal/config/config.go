// Package config 读写 sysbox 的用户配置，位于 ~/.config/sysbox/config.json。
// 所有字段都可省略，省略时使用各工具的默认值。
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config 用户配置
type Config struct {
	Theme    string   `json:"theme,omitempty"` // auto / light / dark
	Agent    Agent    `json:"agent"`
	Claude   Claude   `json:"claude"`
	Obsidian Obsidian `json:"obsidian"`
	Update   Update   `json:"update"`
}

// Agent agent 垃圾清理
type Agent struct {
	KeepDays int `json:"keep_days,omitempty"` // 缓存与日志保留天数，默认 30
}

// Claude Claude Code 更新
type Claude struct {
	Proxy  string `json:"proxy,omitempty"` // 默认 http://127.0.0.1:7890
	Direct bool   `json:"direct,omitempty"`
}

// Obsidian 目录链接
type Obsidian struct {
	Src      string   `json:"src,omitempty"`
	Dest     string   `json:"dest,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

// Update sysbox 自身的升级检查
type Update struct {
	DisableCheck bool `json:"disable_check,omitempty"` // 关闭启动时的新版本检查
}

// Dir 配置目录，遵循 XDG_CONFIG_HOME，默认 ~/.config/sysbox
func Dir() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sysbox"), nil
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
