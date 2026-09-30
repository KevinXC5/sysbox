package screens

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	resourceLogMaxLines = 2000
	resourceLogMaxBytes = 2 * 1024 * 1024
)

type resourceLogEntry struct {
	line string
	done bool
	err  error
}
type resourceLogMsg struct {
	seq   int
	lines []string
	done  bool
	err   error
}

func (p *resourcePage) stopLogs() {
	p.logSeq++
	if p.logCancel != nil {
		p.logCancel()
		p.logCancel = nil
	}
	p.logAlive = false
}
func (p *resourcePage) startLogs() tea.Cmd {
	item, ok := p.current()
	if !ok {
		return nil
	}
	p.stopLogs()
	p.focus = resourceFocusRight
	ctx, cancel := context.WithCancel(p.ctx)
	p.logCancel = cancel
	p.pane, p.outputTitle, p.outputText = "logs", "实时日志", "等待新日志…"
	p.logAlive, p.logAutoScroll = true, true
	p.logLines = nil
	p.logBytes, p.logReceived, p.logPauseAt = 0, 0, 0
	p.logError = nil
	p.detailQuery = ""
	p.err = nil
	p.notice = ""
	p.output.GotoTop()
	client, kind := *p.client, p.kinds[p.tab]
	ch := make(chan resourceLogEntry, 256)
	p.logCh = ch
	go func() {
		defer close(ch)
		send := func(entry resourceLogEntry) {
			select {
			case ch <- entry:
			case <-ctx.Done():
			}
		}
		err := client.StreamLogs(ctx, kind, item, func(line string) { send(resourceLogEntry{line: line}) })
		send(resourceLogEntry{done: true, err: err})
	}()
	return p.waitLogs()
}
func (p *resourcePage) waitLogs() tea.Cmd {
	ch, seq := p.logCh, p.logSeq
	return func() tea.Msg {
		entry, ok := <-ch
		msg := resourceLogMsg{seq: seq}
		if !ok {
			msg.done = true
			return msg
		}
		add := func(entry resourceLogEntry) {
			if entry.done {
				msg.done, msg.err = true, entry.err
			} else {
				msg.lines = append(msg.lines, entry.line)
			}
		}
		add(entry)
		// 一次处理一批日志，避免高频输出逐行触发重绘。
		for !msg.done && len(msg.lines) < 200 {
			select {
			case entry, ok := <-ch:
				if !ok {
					msg.done = true
					return msg
				}
				add(entry)
			default:
				return msg
			}
		}
		return msg
	}
}
func (p *resourcePage) appendLogs(lines []string) {
	for _, line := range lines {
		line = resourceText(line)
		p.logLines = append(p.logLines, line)
		p.logBytes += len(line) + 1
		p.logReceived++
	}
	// 长时间查看仍保持有限缓存；暂停滚动时保留阅读快照，后台继续接收新日志。
	remove := 0
	for len(p.logLines)-remove > resourceLogMaxLines || p.logBytes > resourceLogMaxBytes {
		if remove >= len(p.logLines) {
			break
		}
		p.logBytes -= len(p.logLines[remove]) + 1
		remove++
	}
	if remove > 0 {
		n := copy(p.logLines, p.logLines[remove:])
		clear(p.logLines[n:])
		p.logLines = p.logLines[:n]
	}
	if p.logAutoScroll {
		p.syncLogs()
	}
}
func (p *resourcePage) syncLogs() {
	p.outputText = strings.Join(p.logLines, "\n")
	if len(p.logLines) == 0 {
		p.outputText = "等待新日志…"
	}
}
func (p *resourcePage) pauseLogScroll() {
	if p.pane == "logs" && p.logAutoScroll {
		p.logAutoScroll = false
		p.logPauseAt = p.logReceived
	}
}
func (p *resourcePage) toggleLogScroll() {
	if p.logAutoScroll {
		p.pauseLogScroll()
	} else {
		p.logAutoScroll = true
		p.syncLogs()
		p.output.GotoBottom()
	}
}
