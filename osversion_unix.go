//go:build !windows

package main

import "golang.org/x/sys/unix"

// osVersion is the kernel's name and release, e.g. Linux 6.9.1.
func osVersion() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Sysname[:]) + " " + unix.ByteSliceToString(u.Release[:])
}
