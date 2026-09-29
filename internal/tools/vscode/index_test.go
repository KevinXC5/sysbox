package vscode

import (
	"os"
	"path/filepath"
	"testing"
)

// 真实布局：索引在扩展目录，不在应用数据根。只放索引时不应整组跳过。
func TestLoadIndexUsesExtRoot(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Code")
	ext := filepath.Join(home, "extensions")
	mustMkdir(t, app)
	mustMkdir(t, ext)
	writeIndex(t, filepath.Join(ext, "extensions.json"), []idxRow{
		{"pub.sample", "1.2.0", "pub.sample-1.2.0"},
	})
	// 应用数据根上的同名文件不能被当成索引
	mustWrite(t, filepath.Join(app, "extensions.json"), []byte("{"))

	st := loadIndex(app, ext)
	if !st.ok {
		t.Fatalf("扩展目录索引应可用：%s", st.reason)
	}
	if st.referenced["pub.sample-1.2.0"] != "1.2.0" {
		t.Fatalf("未读到扩展目录索引：%v", st.referenced)
	}
}

func TestLoadIndexNullIsInvalid(t *testing.T) {
	home := t.TempDir()
	ext := filepath.Join(home, "extensions")
	mustMkdir(t, ext)
	mustWrite(t, filepath.Join(ext, "extensions.json"), []byte("null"))
	st := loadIndex(filepath.Join(home, "Code"), ext)
	if st.ok {
		t.Fatal("null 不能当成有效空索引")
	}
}

func TestLoadIndexProfileUnreadableFailsClosed(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Code")
	ext := filepath.Join(home, "extensions")
	mustMkdir(t, ext)
	writeIndex(t, filepath.Join(ext, "extensions.json"), []idxRow{{"pub.sample", "1.2.0", "pub.sample-1.2.0"}})

	// 配置档索引是目录，不是普通文件
	bad := filepath.Join(app, "User", "profiles", "work", "extensions.json")
	mustMkdir(t, bad)
	st := loadIndex(app, ext)
	if st.ok {
		t.Fatal("无法作为普通文件读取的配置档索引必须整组跳过")
	}

	// 配置档目录本身是符号链接
	outside := filepath.Join(home, "elsewhere")
	mustMkdir(t, outside)
	link := filepath.Join(app, "User", "profiles", "linked")
	if err := os.RemoveAll(filepath.Join(app, "User", "profiles", "work")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	st = loadIndex(app, ext)
	if st.ok {
		t.Fatal("符号链接配置档必须整组跳过")
	}
}

func TestLoadIndexProfileReference(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Code")
	ext := filepath.Join(home, "extensions")
	mustMkdir(t, ext)
	writeIndex(t, filepath.Join(ext, "extensions.json"), []idxRow{{"pub.sample", "1.2.0", "pub.sample-1.2.0"}})
	profile := filepath.Join(app, "User", "profiles", "work")
	mustMkdir(t, profile)
	writeIndex(t, filepath.Join(profile, "extensions.json"), []idxRow{{"pub.sample", "1.0.0", "pub.sample-1.0.0"}})

	st := loadIndex(app, ext)
	if !st.ok {
		t.Fatal(st.reason)
	}
	if st.referenced["pub.sample-1.0.0"] != "1.0.0" || st.referenced["pub.sample-1.2.0"] != "1.2.0" {
		t.Fatalf("配置档引用未并入：%v", st.referenced)
	}
}
