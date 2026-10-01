package sysx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// WriterRunner 把二进制标准输出直接写入目标，适用于镜像等大文件。
type WriterRunner interface {
	RunTo(context.Context, Cmd, io.Writer) error
}

func (ExecRunner) RunTo(ctx context.Context, c Cmd, writer io.Writer) error {
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, c.Name, c.Args...)
	command.Stdout, command.Stderr = writer, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if line := firstLine(stderr.String()); line != "" {
			return fmt.Errorf("%w：%s", err, line)
		}
		return err
	}
	return nil
}
