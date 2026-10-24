//go:build !windows

package core

// longPath is a no-op where 8.3 short names do not exist.
func longPath(p string) string { return p }
