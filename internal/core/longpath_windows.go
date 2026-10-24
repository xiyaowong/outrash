//go:build windows

package core

import "syscall"

// longPath expands 8.3 short names (C:\Users\JOHNDO~1) to the long form the
// directory actually has, so a lexical comparison cannot be aliased around.
// A path that does not resolve comes back unchanged.
func longPath(p string) string {
	ptr, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	n, _ := syscall.GetLongPathName(ptr, nil, 0)
	if n == 0 {
		return p
	}
	buf := make([]uint16, n)
	m, err := syscall.GetLongPathName(ptr, &buf[0], n)
	if err != nil || m == 0 {
		return p
	}
	return syscall.UTF16ToString(buf[:m])
}
