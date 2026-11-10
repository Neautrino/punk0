//go:build windows

package metrics

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters is PROCESS_MEMORY_COUNTERS (psapi.h).
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// K32GetProcessMemoryInfo is psapi's GetProcessMemoryInfo, exported by
// kernel32 since Windows 7; x/sys/windows does not wrap it.
var procGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// nativeTable snapshots every process with Toolhelp32: pid and parent pid
// from the snapshot, working set (as RSS), CPU time and start time from the
// process itself. A process this user cannot open (another user's, a
// protected one) is listed unmeasured, so the trees through it stay whole.
func nativeTable() Table {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	t := Table{}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		p := Proc{PPID: int(e.ParentProcessID)}
		measure(e.ProcessID, &p)
		t[int(e.ProcessID)] = p
	}
	if len(t) == 0 {
		return nil
	}
	return t
}

func measure(pid uint32, p *Proc) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) == nil {
		p.start = created.Nanoseconds()
		p.CPU = filetimeSpan(kernel) + filetimeSpan(user)
	}
	mc := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	if ok, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.cb)); ok != 0 {
		p.RSS = uint64(mc.WorkingSetSize)
	}
}

// filetimeSpan reads a FILETIME that holds a duration (GetProcessTimes'
// kernel and user times), in 100 ns units.
func filetimeSpan(ft windows.Filetime) time.Duration {
	return time.Duration(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) * 100
}
