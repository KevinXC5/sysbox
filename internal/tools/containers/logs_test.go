package containers

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type logRunner struct{ command sysx.Cmd }

func (r *logRunner) Run(context.Context, sysx.Cmd) (string, error) { return "", nil }
func (r *logRunner) Stream(_ context.Context, c sysx.Cmd, emit func(string)) error {
	r.command = c
	emit("first-line")
	emit("second-line")
	return nil
}
func TestDockerLogsStreamPinnedContext(t *testing.T) {
	r := &logRunner{}
	c := New(false)
	c.Runner = r
	c.Target = "test"
	var lines []string
	err := c.StreamLogs(context.Background(), "containers", Item{ID: "abc"}, func(line string) { lines = append(lines, line) })
	if err != nil || !reflect.DeepEqual(lines, []string{"first-line", "second-line"}) || !reflect.DeepEqual(r.command.Args, []string{"--context", "test", "logs", "--follow", "--tail", "200", "--timestamps", "abc"}) {
		t.Fatalf("Docker 实时日志未正确绑定资源：%v，%v", r.command.Args, err)
	}
}
func TestKubeLogStreamPinnedNamespaceAndNoTimeout(t *testing.T) {
	r := &logRunner{}
	c := New(true)
	c.Runner = r
	c.Target = "production"
	err := c.StreamLogs(context.Background(), "pods", Item{Name: "app", Namespace: "team"}, func(string) {})
	command := r.command.String()
	if err != nil || !strings.Contains(command, "--context production --request-timeout=0 --namespace team logs --follow pods/app") || !strings.Contains(command, "--all-containers=true") {
		t.Fatalf("Kubernetes 日志上下文或超时设置错误：%s，%v", command, err)
	}
}
