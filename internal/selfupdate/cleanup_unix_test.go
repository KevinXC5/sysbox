//go:build !windows

package selfupdate

import "testing"

func TestCleanupOldNoop(t *testing.T) {
	// Unix 没有 .old 残留，调用不应 panic
	CleanupOld()
}
