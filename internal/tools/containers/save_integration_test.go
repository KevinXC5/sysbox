package containers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

func TestDockerSaveIntegration(t *testing.T) {
	image := os.Getenv("SYSBOX_DOCKER_TEST_SAVE_IMAGE")
	if image == "" {
		t.Skip("设置 SYSBOX_DOCKER_TEST_SAVE_IMAGE 使用已有镜像实测归档")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := New(false)
	result, err := c.SaveImages(ctx, []Item{{ID: image, Name: image}}, t.TempDir(), false, nil)
	if err != nil || len(result.Files) != 1 {
		t.Fatalf("实测打包失败：%+v %v", result, err)
	}
	file, err := os.Open(result.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	found := false
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != "manifest.json" {
			continue
		}
		var manifests []struct{ RepoTags []string }
		if err := json.NewDecoder(archive).Decode(&manifests); err != nil {
			t.Fatal(err)
		}
		for _, manifest := range manifests {
			for _, tag := range manifest.RepoTags {
				if tag == image {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("归档应包含所选镜像标签，可供 docker load 使用")
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		t.Fatalf("gzip 校验失败：%v", err)
	}
}
