//go:build !windows

package sysx

import "context"

// Procs 用 ps 列出当前所有进程。
func Procs(ctx context.Context, r Runner) ([]Proc, error) {
	out, err := r.Run(ctx, C("ps", "-axo", psFields))
	if err != nil {
		return nil, err
	}
	return ParsePS(out), nil
}
