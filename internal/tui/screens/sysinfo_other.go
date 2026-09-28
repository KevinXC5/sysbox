//go:build !darwin && !linux && !windows

package screens

func readSysInfo() sysInfo { return sysInfo{} }
