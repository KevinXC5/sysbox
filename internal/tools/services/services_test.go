package services

import (
	"context"
	"github.com/KevinXC5/sysbox/internal/sysx"
	"testing"
)

type recordingRunner struct {
	commands []sysx.Cmd
	output   string
}

func (r *recordingRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	r.commands = append(r.commands, c)
	return r.output, nil
}
func TestDryRunDoesNotExecute(t *testing.T) {
	runner := &recordingRunner{}
	client := &Client{Runner: runner}
	action := Action{Command: sysx.C("service-tool", "stop", "service"), Mutates: true}
	if _, e := client.Execute(context.Background(), action, true); e != nil {
		t.Fatal(e)
	}
	if len(runner.commands) != 0 {
		t.Fatal("演练模式不能执行修改")
	}
	action.Mutates = false
	if _, e := client.Execute(context.Background(), action, true); e != nil {
		t.Fatal(e)
	}
	if len(runner.commands) != 1 {
		t.Fatal("演练模式应允许查询")
	}
}
