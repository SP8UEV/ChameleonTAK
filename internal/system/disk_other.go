//go:build !linux

package system

func diskUsage(string) (total, used uint64) { return 0, 0 }
