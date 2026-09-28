//go:build !linux

package claudeupdate

// muslLibc 只有 Linux 需要区分 glibc 与 musl
func muslLibc() bool { return false }
