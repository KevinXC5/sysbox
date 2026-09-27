package claudeupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// 下载的单次尝试结果
var (
	errRestart = errors.New("需要从头下载") // 缓存已被清空，下一次从头开始
	errStalled = errors.New("下载速度过低")
)

// fatalError 不应重试的错误
type fatalError struct{ error }

// download 带断点续传和重试的下载，完成后校验 SHA-256
func (u *Updater) download(ctx context.Context, url, part, checksum string) error {
	if err := regularOrMissing(part); err != nil {
		return err
	}
	for attempt := 1; attempt <= u.opt.Attempts; attempt++ {
		if matches(part, checksum) {
			return nil
		}
		u.emit(Event{Stage: StageDownload, Attempt: attempt,
			Msg: fmt.Sprintf("下载二进制，第 %d/%d 次尝试", attempt, u.opt.Attempts)})
		err := u.fetchOnce(ctx, url, part, attempt)
		if err == nil {
			if matches(part, checksum) {
				return nil
			}
			_ = os.Remove(part)
			return errors.New("SHA-256 校验失败，已删除损坏的缓存，未安装该文件")
		}
		var fatal fatalError
		if errors.As(err, &fatal) {
			return fatal.error
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		u.emit(Event{Stage: StageDownload, Attempt: attempt, Warn: true, Msg: err.Error()})
		if attempt < u.opt.Attempts && !errors.Is(err, errRestart) {
			if err := sleep(ctx, u.opt.RetryDelay); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("下载失败，已保留续传缓存：%s", part)
}

// fetchOnce 单次下载：有缓存时带 Range 续传，服务器不支持续传时在本次改为从头写入
func (u *Updater) fetchOnce(ctx context.Context, url, part string, attempt int) error {
	var offset int64
	if fi, err := os.Stat(part); err == nil {
		offset = fi.Size()
	}
	actx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, url, nil)
	if err != nil {
		return fatalError{err}
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("网络错误：%w", err)
	}
	defer resp.Body.Close()

	flags := os.O_WRONLY | os.O_CREATE
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if start := rangeStart(resp.Header.Get("Content-Range")); start != offset {
			_ = os.Remove(part)
			return fmt.Errorf("续传位置不一致，%w", errRestart)
		}
		flags |= os.O_APPEND
	case http.StatusOK:
		if offset > 0 {
			u.emit(Event{Stage: StageDownload, Attempt: attempt, Warn: true, Msg: "服务器不支持续传，改为从头下载"})
		}
		offset = 0
		flags |= os.O_TRUNC
	case http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return fmt.Errorf("缓存超出远端范围，%w", errRestart)
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("服务暂不可用，HTTP %d", resp.StatusCode)
	default:
		return fatalError{fmt.Errorf("下载失败，HTTP %d", resp.StatusCode)}
	}

	total := offset + resp.ContentLength
	if resp.ContentLength < 0 {
		total = -1
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fatalError{err}
	}
	defer f.Close()
	return u.copyWatched(actx, cancel, f, resp.Body, offset, total, attempt)
}

// copyWatched 边写边汇报进度；每个观察窗口内下载量不足即中止本次尝试
func (u *Updater) copyWatched(ctx context.Context, cancel context.CancelFunc, w io.Writer, r io.Reader, done, total int64, attempt int) error {
	var windowBytes atomic.Int64
	var stalled atomic.Bool
	ticker := time.NewTicker(u.opt.StallWindow)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if windowBytes.Swap(0) < u.opt.StallBytes {
					stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	buf := make([]byte, 64*1024)
	lastEmit := time.Time{}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return fatalError{werr}
			}
			done += int64(n)
			windowBytes.Add(int64(n))
			if time.Since(lastEmit) > 100*time.Millisecond {
				u.emit(Event{Stage: StageDownload, Attempt: attempt, Downloaded: done, Total: total})
				lastEmit = time.Now()
			}
		}
		if err == io.EOF {
			u.emit(Event{Stage: StageDownload, Attempt: attempt, Downloaded: done, Total: total})
			if total > 0 && done < total {
				return errors.New("连接提前断开")
			}
			return nil
		}
		if err != nil {
			if stalled.Load() {
				return errStalled
			}
			return fmt.Errorf("连接中断：%w", err)
		}
	}
}

// rangeStart 解析 Content-Range: bytes 100-199/200 的起始位置
func rangeStart(h string) int64 {
	h = strings.TrimPrefix(h, "bytes ")
	start, _, ok := strings.Cut(h, "-")
	if !ok {
		return -1
	}
	n, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return -1
	}
	return n
}
