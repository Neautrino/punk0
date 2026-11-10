//go:build windows

package metrics

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// A child of this process is in the snapshot, measured, and summed into this
// process's tree: what the RAM panel needs to count an agent's processes.
func TestWindowsTreeHoldsChildren(t *testing.T) {
	if os.Getenv("PUNK0_PROCTABLE_CHILD") != "" {
		time.Sleep(time.Minute)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsTreeHoldsChildren$")
	cmd.Env = append(os.Environ(), "PUNK0_PROCTABLE_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	tbl := Processes()
	self, child := tbl[os.Getpid()], tbl[cmd.Process.Pid]
	if child.PPID != os.Getpid() || child.RSS == 0 {
		t.Fatalf("child %d: %+v", cmd.Process.Pid, child)
	}
	if self.start == 0 || child.start < self.start {
		t.Fatalf("start times: self %d, child %d", self.start, child.start)
	}
	if self.CPU == 0 {
		t.Fatalf("self has no CPU time: %+v", self)
	}
	if tree := tbl.Tree(os.Getpid()); tree.RSS < self.RSS+child.RSS {
		t.Fatalf("tree RSS %d, want at least %d + %d", tree.RSS, self.RSS, child.RSS)
	}
}
