package devcache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnsafeCacheRootsLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS fixture")
	}
	for _, key := range []string{"PIP_CACHE_DIR", "UV_CACHE_DIR", "BUN_INSTALL_CACHE_DIR", "YARN_CACHE_FOLDER", "HOMEBREW_CACHE"} {
		t.Run(key, func(t *testing.T) {
			s, home, _ := fixture(t)
			for _, target := range []string{home, filepath.Join(home, "Library"), filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Library", "Caches", "..", ".."), string(filepath.Separator)} {
				s.loc.getenv = func(k string) string {
					if k == key {
						return target
					}
					return ""
				}
				items, err := s.Scan(func(string) {})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, it := range items {
					if it.Path != filepath.Clean(target) {
						continue
					}
					found = true
					if it.Selectable || it.Selected || it.Category != CatSkip {
						t.Fatalf("unsafe target selectable: %+v", it)
					}
					// Even a forged selection and genuine identity must not bypass Remove.
					it.Selectable = true
					it.Category = CatClean
					if err := s.Remove(it); err == nil {
						t.Fatal("unsafe target accepted for removal")
					}
				}
				if !found {
					t.Fatalf("unsafe target should remain visible: %s", target)
				}
			}
		})
	}
}

func TestCacheAncestorSymlinkEscapeLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges")
	}
	s, home, _ := fixture(t)
	outside := t.TempDir()
	mustMkdir(t, filepath.Join(outside, "pip"))
	link := filepath.Join(home, "Library", "Caches", "redirect")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(link, "pip")
	s.loc.getenv = func(k string) string {
		if k == "PIP_CACHE_DIR" {
			return target
		}
		return ""
	}
	it := scan(t, s)["pip/下载缓存"]
	if it.Selectable {
		t.Fatalf("escaping ancestor accepted: %+v", it)
	}
}

func TestCustomCacheLockedAndKnownCacheAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS fixture")
	}
	s, home, _ := fixture(t)
	custom := filepath.Join(home, "custom-cache")
	mustMkdir(t, custom)
	s.loc.getenv = func(k string) string {
		if k == "PIP_CACHE_DIR" {
			return custom
		}
		return ""
	}
	if it := scan(t, s)["pip/下载缓存"]; it.Selectable {
		t.Fatal("unverified custom cache selectable")
	}
	s.loc.getenv = func(string) string { return "" }
	it := scan(t, s)["pip/下载缓存"]
	if !it.Selectable {
		t.Fatal("known cache locked")
	}
	if err := s.Remove(it); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(it.Path); !os.IsNotExist(err) {
		t.Fatalf("cache not removed: %v", err)
	}
}
