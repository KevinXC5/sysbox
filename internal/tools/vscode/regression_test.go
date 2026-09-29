package vscode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecheckProtectsChangedReferencesAndLatest(t *testing.T) {
	o := fixture(t)
	ch := o.Channels[0]
	old := filepath.Join(ch.ExtRoot, "pub.sample-1.0.0")
	profile := filepath.Join(ch.AppRoot, "User", "profiles", "work", "extensions.json")
	writeIndex(t, profile, []idxRow{{"pub.sample", "1.0.0", "pub.sample-1.0.0"}})
	if err := o.Remove(old); err == nil {
		t.Fatal("新加入的配置档引用必须阻止删除")
	}
	if err := os.Remove(profile); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.1.0", "1.2.0"} {
		if err := os.RemoveAll(filepath.Join(ch.ExtRoot, "pub.sample-"+version)); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.Remove(old); err == nil {
		t.Fatal("较新副本消失后必须保留剩余最高版本")
	}
}

func TestUnknownCachesStayProtected(t *testing.T) {
	o := fixture(t)
	for _, name := range []string{"Service Worker", "blob_storage", "DawnUnknownCache"} {
		p := filepath.Join(o.Channels[0].AppRoot, name)
		mustMkdir(t, p)
		if err := o.Remove(p); err == nil {
			t.Fatalf("未知存储不能删除：%s", name)
		}
	}
}

func TestNestedServerVersionAndSelection(t *testing.T) {
	o := fixture(t)
	root := o.Channels[0].CLIRoots[0]
	p := filepath.Join(root, "Stable-old", "server", "package.json")
	mustWrite(t, p, []byte(`{"version":"1.70.0"}`))
	items, err := Classify(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Name == "Stable-old" {
			if !it.Selectable || it.Selected || it.Category != CatSkip {
				t.Fatal("旧服务端应允许手动勾选，默认不选")
			}
			return
		}
	}
	t.Fatal("未识别嵌套的 CLI 服务端安装")
}
