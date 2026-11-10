//go:build !windows

package metrics

// nativeTable is Windows' Toolhelp32 snapshot; elsewhere /proc or ps answers.
func nativeTable() Table { return nil }
