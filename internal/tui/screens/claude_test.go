package screens

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
)

// 完成消息携带实际耗时，后续刷新不能把停留时间累计到安装用时中。
func TestClaudeCompletionStopsTiming(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := &claudePage{home: t.TempDir(), state: claudeRunning, proxyReady: true, spin: spinner.New()}
		msg := claudeDoneMsg{elapsed: 19700 * time.Millisecond}
		if failed {
			msg.err = errors.New("安装失败")
		}
		m.Update(msg)
		if m.Busy() || m.elapsed != msg.elapsed {
			t.Fatal("任务结束后应退出运行状态并固定用时")
		}
		before := m.viewRun(120, 30)
		if !failed && !strings.Contains(before, "用时 19.7 秒") {
			t.Fatal("完成页面未显示任务实际用时")
		}
		if cmd := m.Update(spinner.TickMsg{Time: time.Now().Add(time.Minute)}); cmd != nil {
			t.Fatal("任务结束后不应继续调度动画")
		}
		if after := m.viewRun(120, 30); after != before {
			t.Fatal("完成页面刷新后用时发生变化")
		}
	}
}
