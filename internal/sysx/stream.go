package sysx

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// StreamingRunner 持续接收命令输出，日志类工具无需等待进程结束。
type StreamingRunner interface {
	Stream(context.Context, Cmd, func(string)) error
}

// Stream 合并标准输出与标准错误，Docker 容器的 stderr 同样属于日志内容。
func (ExecRunner) Stream(ctx context.Context, c Cmd, emit func(string)) error {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	// 主进程退出前若后代仍持有管道，取消也必须及时中断读取。
	go func() { <-ctx.Done(); _ = output.Close() }()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	last := ""
	for scanner.Scan() {
		last = scanner.Text()
		emit(last)
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := parent.Err(); err != nil {
		return err
	}
	if scanErr != nil {
		return fmt.Errorf("读取输出失败：%w", scanErr)
	}

	if waitErr != nil {
		if text := strings.TrimSpace(last); text != "" {
			return fmt.Errorf("%w：%s", waitErr, text)
		}
		return waitErr
	}
	return nil
}
