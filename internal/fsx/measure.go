package fsx

import "sync"

// MeasureAll 并发统计多个路径的磁盘占用，每完成一项回调一次。
// 回调可能来自多个 goroutine，调用方需自行保证并发安全。
func MeasureAll(paths []string, workers int, done func(i int, size int64)) {
	workers = max(1, workers)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p string) {
			defer func() { <-sem; wg.Done() }()
			done(i, DiskUsage(p))
		}(i, p)
	}
	wg.Wait()
}
