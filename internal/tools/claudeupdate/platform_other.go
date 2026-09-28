//go:build !darwin

package claudeupdate

// rosettaTranslated 只有 macOS 存在 Rosetta 转译
func rosettaTranslated() bool { return false }
