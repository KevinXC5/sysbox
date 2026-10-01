package containers

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type saveRunner struct {
	calls []sysx.Cmd
	fail  bool
}

func (r *saveRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	r.calls = append(r.calls, c)
	return `{"Id":"sha256:1234567890abcdef","Created":"2026-10-01T01:02:03.123Z"}`, nil
}
func (r *saveRunner) RunTo(_ context.Context, c sysx.Cmd, w io.Writer) error {
	r.calls = append(r.calls, c)
	_, err := w.Write([]byte("镜像的二进制 tar 内容\x00\xff"))
	if r.fail {
		return errors.New("导出失败")
	}
	return err
}
func TestImageArchiveName(t *testing.T) {
	created, _ := time.Parse(time.RFC3339, "2026-10-01T01:02:03Z")
	got := ImageArchiveName(Item{Name: "registry:5000/team/app:v1"}, "sha256:1234567890abcdef", created)
	if got != "app_v1_1234567890ab_2026-10-01-09-02-03.tar.gz" {
		t.Fatalf("未遵循 SC-Helm 格式：%s", got)
	}
	got = ImageArchiveName(Item{Name: "<none>:<none>"}, "sha256:1234567890abcdef", created)
	if strings.ContainsAny(got, `<>:\/`) || !strings.HasPrefix(got, "untagged_none_") {
		t.Fatal("无标签镜像也应有跨平台文件名")
	}
}
func TestSaveImagesStreamAndNoOverwrite(t *testing.T) {
	c := New(false)
	c.Target = "remote"
	r := &saveRunner{}
	c.Runner = r
	dir := t.TempDir()
	items := []Item{{ID: "team/app:v1", Name: "team/app:v1"}}
	result, err := c.SaveImages(context.Background(), items, dir, false, nil)
	if err != nil || len(result.Files) != 1 {
		t.Fatalf("打包失败：%+v %v", result, err)
	}
	if r.calls[1].String() != "docker --context remote image save team/app:v1" {
		t.Fatal("打包应固定当前环境并保留镜像标签")
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
	data, err := io.ReadAll(gz)
	if err != nil || string(data) != "镜像的二进制 tar 内容\x00\xff" {
		t.Fatal("流式压缩不能损坏二进制内容")
	}
	before, _ := os.ReadFile(result.Files[0])
	if _, err := c.SaveImages(context.Background(), items, dir, false, nil); err == nil {
		t.Fatal("不得覆盖同名压缩包")
	}
	after, _ := os.ReadFile(result.Files[0])
	if string(before) != string(after) {
		t.Fatal("已有文件不能被修改")
	}
}
func TestSaveImagesFailureCleanupAndDryRun(t *testing.T) {
	c := New(false)
	r := &saveRunner{fail: true}
	c.Runner = r
	dir := t.TempDir()
	items := []Item{{ID: "app:v1", Name: "app:v1"}, {ID: "other:v2", Name: "other:v2"}}
	result, err := c.SaveImages(context.Background(), items, dir, false, nil)
	files, _ := os.ReadDir(dir)
	if err == nil || len(result.Failures) != 2 || len(files) != 0 {
		t.Fatal("逐项失败后应继续，且不留下未完成归档")
	}
	r.calls = nil
	dir = filepath.Join(dir, "不存在")
	result, err = c.SaveImages(context.Background(), items, dir, true, nil)
	if err != nil || len(result.Files) != 2 || len(r.calls) != 2 {
		t.Fatal("演练只读取元数据，不能导出镜像")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("演练不能创建保存目录")
	}
}
