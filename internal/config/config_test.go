package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// useTempConfig 把配置目录指到临时目录，避免读到用户真实配置。
func useTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
		return
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestLoadMissing(t *testing.T) {
	useTempConfig(t)
	c, err := Load()
	if err != nil || c.Theme != "" {
		t.Fatalf("配置文件不存在时应返回零值，实际 %+v, %v", c, err)
	}
}

func TestSaveLoad(t *testing.T) {
	useTempConfig(t)
	if err := Save(Config{Theme: "light"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil || c.Theme != "light" {
		t.Fatalf("读回的配置不符：%+v, %v", c, err)
	}
}

func TestUnixConfigBaseUsesXDG(t *testing.T) {
	got, err := unixConfigBase(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/custom/config"
		}
		return ""
	})
	if err != nil || got != "/custom/config" {
		t.Fatalf("应使用 XDG_CONFIG_HOME，实际 %q %v", got, err)
	}
}

func TestWindowsConfigBaseUsesAppData(t *testing.T) {
	got, err := windowsConfigBase(func(k string) string {
		if k == "APPDATA" {
			return `C:\Users\me\AppData\Roaming`
		}
		return ""
	})
	if err != nil || got != `C:\Users\me\AppData\Roaming` {
		t.Fatalf("应使用 APPDATA，实际 %q %v", got, err)
	}
}

func TestWindowsConfigBaseFallsBack(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "AppData", "Roaming")
	got, err := windowsConfigBase(func(string) string { return "" })
	if err != nil || got != want {
		t.Fatalf("APPDATA 为空应回退到家目录下的 AppData\\Roaming，实际 %q，期望 %q，%v", got, want, err)
	}
}

func TestConfigBaseFollowsOS(t *testing.T) {
	lookup := func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return "/xdg"
		case "APPDATA":
			return `C:\AppData`
		default:
			return ""
		}
	}
	got, err := configBase(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if got != `C:\AppData` {
			t.Fatalf("Windows 应走 APPDATA，实际 %q", got)
		}
		return
	}
	if got != "/xdg" {
		t.Fatalf("非 Windows 应走 XDG，实际 %q", got)
	}
}
