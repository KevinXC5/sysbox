package fsx

import (
	"context"
	"sync"
)

// MeasureAllContext 并发统计多个路径的磁盘占用，每完成一项回调一次；取消后不再回调。
// 回调可能来自多个 goroutine，调用方需自行保证并发安全。
func MeasureAllContext(ctx context.Context, paths []string, workers int, done func(i int, u Usage)) {
	workers = min(len(paths), max(1, workers))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				u := DiskUsageContext(ctx, paths[i])
				if ctx.Err() == nil {
					done(i, u)
				}
			}
		}()
	}
dispatch:
	for i := range paths {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}
