package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// osVersion is the Windows version and build, e.g. 10.0.26100.
func osVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
