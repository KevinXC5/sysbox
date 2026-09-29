//go:build !windows

package uninstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRemovesExecutableAndConfig(t *testing.T) {
	cfg := useTempConfig(t)
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "sysbox")
	other := filepath.Join(dir, "other")
	for _, f := range []string{exe, other} {
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	p, err := NewPlan(exe, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{exe, cfg} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s 应已删除", f)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("同目录下的其他文件不应被删除")
	}
}
