package config

import "testing"

func TestLoadMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil || c.Theme != "" {
		t.Fatalf("配置文件不存在时应返回零值，实际 %+v, %v", c, err)
	}
}

func TestSaveLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Config{Theme: "light"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil || c.Theme != "light" {
		t.Fatalf("读回的配置不符：%+v, %v", c, err)
	}
}
