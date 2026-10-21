package metrics

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Proc is one process in a Table snapshot.
type Proc struct {
	PPID int
	RSS  uint64        // resident set size in bytes
	CPU  time.Duration // user + system time consumed so far
}

// Table is a snapshot of every process the user can see, keyed by pid.
type Table map[int]Proc

// clkTck is the kernel's USER_HZ, the unit of /proc/<pid>/stat CPU times; it
// is 100 on every Linux architecture Go supports.
const clkTck = 100

// Processes snapshots the process table: /proc on Linux, ps(1) elsewhere. It
// returns nil where neither is available (Windows).
func Processes() Table {
	if t := procfsTable(); t != nil {
		return t
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,rss=,time=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(out))
}

// Tree sums the RSS and CPU time of pid and every process below it.
func (t Table) Tree(pid int) Proc {
	kids := make(map[int][]int, len(t))
	for p, proc := range t {
		kids[proc.PPID] = append(kids[proc.PPID], p)
	}
	var sum Proc
	seen := map[int]bool{}
	stack := []int{pid}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		proc, ok := t[p]
		if !ok || seen[p] {
			continue
		}
		seen[p] = true
		sum.RSS += proc.RSS
		sum.CPU += proc.CPU
		stack = append(stack, kids[p]...)
	}
	return sum
}

func procfsTable() Table {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	page := uint64(os.Getpagesize())
	t := Table{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // exited since ReadDir
		}
		if p, ok := parseStat(string(data), page); ok {
			t[pid] = p
		}
	}
	if len(t) == 0 {
		return nil
	}
	return t
}

// parseStat reads ppid, utime, stime and rss from a /proc/<pid>/stat line.
// The command name is in parentheses and may hold spaces or parentheses, so
// fields are counted from the last ')'.
func parseStat(line string, page uint64) (Proc, bool) {
	i := strings.LastIndexByte(line, ')')
	if i < 0 {
		return Proc{}, false
	}
	f := strings.Fields(line[i+1:])
	if len(f) < 22 {
		return Proc{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	utime, err2 := strconv.ParseUint(f[11], 10, 64)
	stime, err3 := strconv.ParseUint(f[12], 10, 64)
	rss, err4 := strconv.ParseInt(f[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return Proc{}, false
	}
	return Proc{
		PPID: ppid,
		RSS:  uint64(max(rss, 0)) * page,
		CPU:  time.Duration(utime+stime) * time.Second / clkTck,
	}, true
}

// parsePS reads `ps -A -o pid=,ppid=,rss=,time=` output; rss is in KiB.
func parsePS(out string) Table {
	t := Table{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		kb, err3 := strconv.ParseUint(f[2], 10, 64)
		cpu, ok := parsePSTime(f[3])
		if err1 != nil || err2 != nil || err3 != nil || !ok {
			continue
		}
		t[pid] = Proc{PPID: ppid, RSS: kb * 1024, CPU: cpu}
	}
	return t
}

// parsePSTime reads ps's cumulative CPU time, [[dd-]hh:]mm:ss[.ff]: macOS
// prints "0:01.25", Linux and the BSDs "00:00:01" or "1-02:03:04".
func parsePSTime(s string) (time.Duration, bool) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, false
	}
	total := sec
	unit := 60.0
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, false
		}
		total += float64(n) * unit
		unit *= 60
	}
	total += float64(days) * 86400
	return time.Duration(total * float64(time.Second)), true
}
