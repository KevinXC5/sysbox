// Package sysxtest 提供测试用的假命令执行器，按命令前缀返回预设输出并记录调用。
package sysxtest

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

// Reply 预设的命令结果
type Reply struct {
	Out string
	Err error
}

// Runner 假执行器：以命令文本前缀匹配 Replies，未匹配的命令返回成功和空输出。
// Passthrough 中前缀匹配的命令交给真实执行器，用于测试里需要真正生效的操作。
type Runner struct {
	mu          sync.Mutex
	Replies     map[string]Reply
	Passthrough []string
	Calls       []string
}

// ErrFailed 用于模拟命令失败
var ErrFailed = errors.New("模拟失败")

// Run 实现 sysx.Runner
func (r *Runner) Run(ctx context.Context, c sysx.Cmd) (string, error) {
	r.mu.Lock()
	s := c.String()
	r.Calls = append(r.Calls, s)
	for _, prefix := range r.Passthrough {
		if strings.HasPrefix(s, prefix) {
			r.mu.Unlock()
			return sysx.ExecRunner{}.Run(ctx, c)
		}
	}
	defer r.mu.Unlock()
	// 取最长的匹配前缀，避免短前缀覆盖更具体的规则
	best, found := "", false
	for prefix := range r.Replies {
		if strings.HasPrefix(s, prefix) && len(prefix) >= len(best) {
			best, found = prefix, true
		}
	}
	if !found {
		return "", nil
	}
	return r.Replies[best].Out, r.Replies[best].Err
}

// Called 是否执行过以 prefix 开头的命令
func (r *Runner) Called(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.Calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}
