package metrics

import (
	"os"
	"testing"
	"time"
)

func TestParsePSTime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"0:01.25":    1250 * time.Millisecond,                 // macOS
		"12:34.50":   12*time.Minute + 34500*time.Millisecond, // macOS, past a minute
		"00:00:07":   7 * time.Second,                         // Linux procps
		"01:02:03":   time.Hour + 2*time.Minute + 3*time.Second,
		"2-01:00:00": 49 * time.Hour,
	} {
		got, ok := parsePSTime(in)
		if !ok || got.Round(time.Millisecond) != want {
			t.Errorf("parsePSTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "7", "a:00", "1:2:3:4", "x-00:00:01"} {
		if _, ok := parsePSTime(in); ok {
			t.Errorf("parsePSTime(%q) accepted", in)
		}
	}
}

// A command name may hold spaces and parentheses; fields count from the last ')'.
func TestParseStat(t *testing.T) {
	line := "4242 (my (odd) cmd) S 7 4242 4242 0 -1 4194304 100 0 0 0 250 50 0 0 20 0 1 0 100 1000000 300 18446744073709551615"
	p, ok := parseStat(line, 4096)
	if !ok {
		t.Fatal("not parsed")
	}
	if p.PPID != 7 || p.CPU != 3*time.Second || p.RSS != 300*4096 {
		t.Fatalf("got %+v", p)
	}
}

func TestTreeSumsDescendantsOnly(t *testing.T) {
	tbl := Table{
		1: {PPID: 0, RSS: 1, CPU: 1},
		2: {PPID: 1, RSS: 10, CPU: 10},
		3: {PPID: 2, RSS: 100, CPU: 100},
		4: {PPID: 1, RSS: 1000, CPU: 1000}, // sibling of 2
		5: {PPID: 6, RSS: 5, CPU: 5},       // a ppid cycle must not loop
		6: {PPID: 5, RSS: 6, CPU: 6},
	}
	if got := tbl.Tree(2); got.RSS != 110 || got.CPU != 110 {
		t.Fatalf("Tree(2) = %+v", got)
	}
	if got := tbl.Tree(5); got.RSS != 11 {
		t.Fatalf("Tree(5) = %+v", got)
	}
	if got := tbl.Tree(99); got.RSS != 0 {
		t.Fatalf("Tree(missing) = %+v", got)
	}
}

func TestProcessesSeesThisProcess(t *testing.T) {
	tbl := Processes()
	if tbl == nil {
		t.Skip("no process table on this platform")
	}
	if p, ok := tbl[os.Getpid()]; !ok || p.RSS == 0 || p.PPID != os.Getppid() {
		t.Fatalf("self: %+v, %v", p, ok)
	}
}

func TestParseRollupPSS(t *testing.T) {
	rollup := "55d0c0a00000-7ffd8a1f2000 ---p 00000000 00:00 0                          [rollup]\nRss:              309160 kB\nPss:              234741 kB\nPss_Dirty:        190000 kB\n"
	if got, ok := parseRollupPSS(rollup); !ok || got != 234741*1024 {
		t.Fatalf("parseRollupPSS = %d, %v", got, ok)
	}
	if _, ok := parseRollupPSS("Rss: 1 kB\n"); ok {
		t.Fatal("a rollup without Pss was read")
	}
}

// This process's PSS is read from smaps_rollup. It is not compared with the
// RSS: that comes from /proc/<pid>/stat at snapshot time, PSS a moment
// later, and a running process grows in between.
func TestTreeReadsPSS(t *testing.T) {
	if _, err := os.Stat("/proc/self/smaps_rollup"); err != nil {
		t.Skip("no smaps_rollup here; PSS falls back to RSS")
	}
	tbl := Processes()
	if self := tbl.Tree(os.Getpid()); self.PSS == 0 {
		t.Fatalf("self tree: no PSS (RSS %d)", self.RSS)
	}
}
