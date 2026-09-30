package screens

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestResourceLiveLogScrollPauseAndResume(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	p.pane = "logs"
	p.focus = resourceFocusRight
	p.outputTitle = "实时日志"
	p.logAlive = true
	p.logAutoScroll = true
	p.logSeq = 1
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("历史日志 %03d", i))
	}
	p.Update(resourceLogMsg{seq: 1, lines: lines})
	p.Body(120, 30)
	if !p.output.AtBottom() {
		t.Fatal("打开实时日志必须自动滚到底部")
	}
	pressResource(p, " ")
	if p.logAutoScroll {
		t.Fatal("空格应关闭自动滚动")
	}
	p.output.ViewUp()
	offset := p.output.YOffset
	old := p.outputText
	p.Update(resourceLogMsg{seq: 1, lines: []string{"新日志 100", "新日志 101"}})
	body := p.Body(120, 30)
	if p.output.YOffset != offset || p.outputText != old || p.logReceived != 102 || !strings.Contains(body, "2 条新日志") {
		t.Fatal("暂停后应保留阅读位置，同时继续接收新日志")
	}
	pressResource(p, " ")
	body = p.Body(120, 30)
	if !p.logAutoScroll || !p.output.AtBottom() || !strings.Contains(body, "新日志 101") {
		t.Fatal("恢复自动滚动应跳回最新日志")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	p.Body(120, 30)
	if p.logAutoScroll {
		t.Fatal("手动翻页应自动暂停跟随")
	}
}
func TestResourceLiveLogBufferLimitAndStaleMessages(t *testing.T) {
	p := newResourcePage(Env{}, false)
	defer p.Close()
	p.pane = "logs"
	p.logAutoScroll = true
	p.logSeq = 10
	p.logAlive = true
	var lines []string
	for i := 0; i < 3000; i++ {
		lines = append(lines, fmt.Sprintf("%04d", i)+strings.Repeat("x", 1000))
	}
	p.Update(resourceLogMsg{seq: 10, lines: lines})
	if len(p.logLines) > resourceLogMaxLines || p.logBytes > resourceLogMaxBytes || !strings.Contains(p.outputText, "2999") {
		t.Fatal("日志缓存应有限且保留最新内容")
	}
	old := p.outputText
	p.Update(resourceLogMsg{seq: 9, lines: []string{"旧资源日志"}, done: true})
	if p.outputText != old || !p.logAlive {
		t.Fatal("旧连接不能覆盖当前日志或结束当前连接")
	}
	p.queueDetails()
	p.Update(resourceLogMsg{seq: 10, lines: []string{"已取消的日志"}})
	if p.pane != "info" || p.outputText != old || p.logAlive {
		t.Fatal("返回详情应关闭实时日志并忽略迟到数据")
	}
}
