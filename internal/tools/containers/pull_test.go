package containers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type allPullRunner struct {
	commands []sysx.Cmd
	fail     bool
	cancel   context.CancelFunc
}

func (r *allPullRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	r.commands = append(r.commands, c)
	if strings.Contains(c.String(), "image ls") {
		return `{"Repository":"app","Tag":"v1","ID":"abc"}
{"Repository":"other","Tag":"v2","ID":"def"}
{"Repository":"app","Tag":"v1","ID":"abc"}
{"Repository":"<none>","Tag":"<none>","ID":"ghi"}`, nil
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.fail && strings.Contains(c.String(), "pull app:v1") {
		return "", errors.New("仓库无权限")
	}
	return "", nil
}
func TestPullAllPinnedDeduplicatedAndPartialFailure(t *testing.T) {
	runner := &allPullRunner{fail: true}
	c := New(false)
	c.Target = "remote"
	c.Runner = runner
	var progress []PullProgress
	result, err := c.PullAll(context.Background(), false, func(p PullProgress) { progress = append(progress, p) })
	if err == nil || result.Total != 2 || result.Succeeded != 1 || result.Skipped != 1 || len(result.Failures) != 1 {
		t.Fatalf("批量拉取结果错误：%+v %v", result, err)
	}
	if len(runner.commands) != 3 || !strings.Contains(runner.commands[2].String(), "--context remote pull other:v2") {
		t.Fatal("全部拉取应去重、固定目标并在单项失败后继续")
	}
	if len(progress) != 2 || progress[1].Done != 1 || progress[1].Total != 2 {
		t.Fatal("应逐项显示进度")
	}
}
func TestPullAllDryRunAndCancellation(t *testing.T) {
	runner := &allPullRunner{}
	c := New(false)
	c.Runner = runner
	result, err := c.PullAll(context.Background(), true, nil)
	if err != nil || result.Total != 2 || len(runner.commands) != 1 {
		t.Fatal("批量拉取演练只能读取镜像列表")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner = &allPullRunner{cancel: cancel}
	c.Runner = runner
	result, err = c.PullAll(ctx, false, nil)
	if !errors.Is(err, context.Canceled) || len(runner.commands) != 2 || result.Succeeded != 1 {
		t.Fatal("取消后不能继续拉取后续镜像")
	}
}
