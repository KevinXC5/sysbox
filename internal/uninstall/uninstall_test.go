package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveEntry(t *testing.T) {
	cases := []struct {
		name, value, dir, want string
		ok                     bool
	}{
		{"开头", `C:\sysbox;C:\bin`, `C:\sysbox`, `C:\bin`, true},
		{"中间", `C:\a;C:\sysbox;C:\b`, `C:\sysbox`, `C:\a;C:\b`, true},
		{"唯一一项", `C:\sysbox`, `C:\sysbox`, ``, true},
		{"忽略大小写与末尾斜杠", `c:\SYSBOX\;C:\bin`, `C:\sysbox`, `C:\bin`, true},
		{"不误删前缀相同的目录", `C:\sysbox2;C:\bin`, `C:\sysbox`, `C:\sysbox2;C:\bin`, false},
		{"其他项原样保留", `%USERPROFILE%\bin;;C:\sysbox`, `C:\sysbox`, `%USERPROFILE%\bin;`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := removeEntry(c.value, c.dir)
			if got != c.want || ok != c.ok {
				t.Fatalf("removeEntry(%q, %q) = %q, %v；期望 %q, %v", c.value, c.dir, got, ok, c.want, c.ok)
			}
		})
	}
}

// useTempConfig 把配置目录指到临时目录，返回 sysbox 配置目录
func useTempConfig(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", base)
	} else {
		t.Setenv("XDG_CONFIG_HOME", base)
	}
	return filepath.Join(base, "sysbox")
}

func TestNewPlanPurge(t *testing.T) {
	cfg := useTempConfig(t)
	exe := filepath.Join(t.TempDir(), "sysbox")

	p, err := NewPlan(exe, true)
	if err != nil || p.ConfigDir != "" {
		t.Fatalf("配置目录不存在时不应列入计划：%+v %v", p, err)
	}
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, _ = NewPlan(exe, false); p.ConfigDir != "" {
		t.Fatalf("未指定 purge 时应保留配置：%+v", p)
	}
	if p, _ = NewPlan(exe, true); p.ConfigDir != cfg {
		t.Fatalf("purge 时应删除配置目录 %s，实际 %+v", cfg, p)
	}
	if len(p.Items()) != 2 {
		t.Fatalf("清单应包含程序与配置目录：%v", p.Items())
	}
}
