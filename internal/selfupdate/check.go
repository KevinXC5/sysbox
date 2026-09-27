package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CheckInterval 自动检查的最短间隔
const CheckInterval = 24 * time.Hour

// cache 上次检查的结果，避免每次启动都访问 GitHub
type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func cachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sysbox", "update-check.json"), nil
}

// CheckCached 返回比 current 新的版本号，没有则返回空串。
// 距上次检查不足 CheckInterval 时直接使用缓存结果，不访问网络。
func (c *Client) CheckCached(ctx context.Context, current string) (string, error) {
	p, err := cachePath()
	if err != nil {
		return "", err
	}
	var ch cache
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &ch)
	}
	if time.Since(ch.CheckedAt) >= CheckInterval {
		rel, err := c.Latest(ctx)
		if err != nil {
			return "", err
		}
		ch = cache{CheckedAt: time.Now(), Latest: rel.Tag}
		if b, err := json.Marshal(ch); err == nil {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, b, 0o644)
		}
	}
	if Newer(ch.Latest, current) {
		return ch.Latest, nil
	}
	return "", nil
}
