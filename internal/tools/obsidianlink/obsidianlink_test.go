package obsidianlink

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	o := Options{
		Src:             filepath.Join(root, "work"),
		Dest:            filepath.Join(root, "vault", "work-link"),
		DefaultExcludes: []string{"archive"},
		StateFile:       filepath.Join(root, "config", "obsidian.selected"),
	}
	for _, d := range []string{"notes", "talks", "archive", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(o.Src, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(o.Dest), 0o755); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestFirstRunUsesDefaultExcludes(t *testing.T) {
	o := setup(t)
	s, err := Load(o)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != StateMissing || len(s.Dirs) != 3 {
		t.Fatalf("现状有误：%+v", s)
	}
	if s.Checked["archive"] || !s.Checked["notes"] {
		t.Errorf("默认勾选有误：%v", s.Checked)
	}
}

func TestApplyFromFullLink(t *testing.T) {
	o := setup(t)
	// 现状是整目录链接
	if err := os.Symlink(o.Src, o.Dest); err != nil {
		t.Fatal(err)
	}
	s, err := Load(o)
	if err != nil || s.State != StateFullLink {
		t.Fatalf("应识别为整目录链接：%v %+v", err, s)
	}
	chosen := map[string]bool{"notes": true}
	p := BuildPlan(o, s, chosen)
	for _, st := range Apply(o, s, p, []string{"notes"}) {
		if st.Err != nil {
			t.Fatalf("%s：%v", st.Title, st.Err)
		}
	}
	if target, _ := os.Readlink(filepath.Join(o.Dest, "notes")); target != filepath.Join(o.Src, "notes") {
		t.Errorf("子目录链接有误：%s", target)
	}
	if fi, _ := os.Lstat(o.Dest); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("库内目标应改为普通文件夹")
	}

	// 再次运行：记录生效，取消勾选会移除链接
	s, _ = Load(o)
	if !s.Checked["notes"] || s.Checked["talks"] {
		t.Errorf("应按记录预勾：%v", s.Checked)
	}
	p = BuildPlan(o, s, map[string]bool{"talks": true})
	if len(p.Add) != 1 || len(p.Remove) != 1 || p.Remove[0] != "notes" {
		t.Errorf("变更计划有误：%+v", p)
	}
}

func TestRealPathIsNeverRemoved(t *testing.T) {
	o := setup(t)
	if err := os.MkdirAll(filepath.Join(o.Dest, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(o)
	p := BuildPlan(o, s, map[string]bool{})
	if len(p.Skip) != 1 || len(p.Remove) != 0 {
		t.Errorf("库内真实目录只能跳过，不能删除：%+v", p)
	}
}

func TestRememberedAndVanished(t *testing.T) {
	o := setup(t)
	if err := os.MkdirAll(filepath.Dir(o.StateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.StateFile, []byte("# 记录\ntalks\nremoved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(o)
	if !s.Checked["talks"] || s.Checked["notes"] {
		t.Errorf("应按记录预勾：%v", s.Checked)
	}
	if len(s.Vanished) != 1 || s.Vanished[0] != "removed" {
		t.Errorf("应标出已消失的目录：%v", s.Vanished)
	}
}
