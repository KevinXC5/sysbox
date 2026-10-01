package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/tools/containers"
)

func TestImageSaveSelectedAndCustomDirectory(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	p.tab = 1
	runner := &resourceRunner{output: `{"Id":"sha256:1234567890abcdef","Created":"2026-10-01T01:02:03Z"}`}
	p.client.Runner = runner
	p.items = []containers.Item{{ID: "a:v1", Name: "a:v1"}, {ID: "b:v2", Name: "b:v2"}, {ID: "c:v3", Name: "c:v3"}}
	p.selected[itemKey(p.items[0])] = true
	p.selected[itemKey(p.items[2])] = true
	p.cursor = 1
	p.filter = "没有匹配项"
	pressResource(p, "E")
	if p.mode != resourceInput || len(p.actionItems) != 2 || p.actionItems[1].ID != "c:v3" {
		t.Fatal("多选打包应使用勾选项，不混入当前行")
	}
	dir := filepath.Join(t.TempDir(), "打包目录")
	p.input.SetValue(dir)
	cmd := pressResource(p, "enter")
	for cmd != nil && p.busy {
		cmd = p.Update(cmd())
	}
	if len(runner.calls) != 2 || !strings.Contains(p.outputText, "a_v1_") || !strings.Contains(p.outputText, "c_v3_") || strings.Contains(p.outputText, "b_v2_") {
		t.Fatal("应显示所选镜像各自的压缩包路径")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("演练不能写入文件")
	}
	if len(p.selected) != 2 {
		t.Fatal("打包不能清除勾选或修改镜像列表")
	}
}
func TestImageSaveOneClickAndContainerExec(t *testing.T) {
	p := newResourcePage(Env{DryRun: true}, false)
	defer p.Close()
	p.tab = 1
	p.client.Runner = &resourceRunner{output: `{"Id":"sha256:1234567890abcdef","Created":"2026-10-01T01:02:03Z"}`}
	p.items = []containers.Item{{ID: "app:v1", Name: "app:v1"}}
	cmd := pressResource(p, "e")
	if cmd == nil || !p.busy || p.mode != resourceList {
		t.Fatal("小写 e 应一键打包，不增加确认或输入步骤")
	}
	for cmd != nil && p.busy {
		cmd = p.Update(cmd())
	}
	if !strings.Contains(p.outputText, filepath.Join("images", "app_v1_")) {
		t.Fatal("默认应保存到启动目录的 images 下")
	}
	p.tab = 0
	p.items = []containers.Item{{ID: "container", Name: "容器", State: "running"}}
	if pressResource(p, "e") == nil {
		t.Fatal("容器的 e 终端操作不能受影响")
	}
}
