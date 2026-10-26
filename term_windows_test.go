//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// screen is everything the shell has printed so far.
func screen(sh *Shell) string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return string(sh.out.Bytes())
}

func waitScreen(t *testing.T, sh *Shell, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(screen(sh), want) {
		if time.Now().After(deadline) {
			t.Fatalf("shell never printed %q; screen:\n%q", want, screen(sh))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func closeWithin(t *testing.T, sh *Shell, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { sh.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("Close did not return")
	}
}

// A tab's first command runs in each Windows shell's own syntax, including
// the fallback agent tabs use when a resume fails, and later environment
// entries win as they do on Unix. The expected text only exists once the
// shell has run the command: it is not in the command itself.
func TestConPTYShellsRunTabCommands(t *testing.T) {
	useSettings(t, map[string]any{})
	for _, c := range []struct {
		name, shell, command, want string
	}{
		{"cmd", "cmd.exe", "echo punk0-%OS%-%PUNK0_T%", "punk0-Windows_NT-two"},
		{"cmd fallback", "cmd.exe", cmdShell.orElse("cmd /c exit 3", "echo fell%OS%"), "fellWindows_NT"},
		{"powershell", "powershell.exe", "Write-Output ('punk0-' + $env:PUNK0_T)", "punk0-two"},
		{"powershell fallback", "powershell.exe", powerShell.orElse("cmd /c exit 3", "Write-Output ('fell' + 'back')"), "fellback"},
		{"powershell no fallback", "powershell.exe", powerShell.orElse("cmd /c exit 0", "Write-Output ('fell' + 'back')") + "; Write-Output ('end' + 'ed')", "ended"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sh, err := startShell(t.TempDir(), c.shell, c.command, []string{"PUNK0_T=one", "punk0_t=two"})
			if err != nil {
				t.Fatal(err)
			}
			defer closeWithin(t, sh, 15*time.Second)
			waitScreen(t, sh, c.want)
			if c.name == "powershell no fallback" && strings.Contains(screen(sh), "fellback") {
				t.Fatal("the fallback ran after a command that succeeded")
			}
			if sh.Exited() {
				t.Fatal("the shell quit after its command instead of staying at its prompt")
			}
		})
	}
}

// Keystrokes reach the shell, and a resize changes the size programs see.
func TestConPTYInputAndResize(t *testing.T) {
	useSettings(t, map[string]any{})
	sh, err := startShell(t.TempDir(), "powershell.exe", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWithin(t, sh, 15*time.Second)
	waitScreen(t, sh, "PS ")
	if err := sh.tty.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := sh.tty.Write([]byte("Write-Output ('w' + $Host.UI.RawUI.WindowSize.Width)\r")); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, sh, "w120")
}

// Closing a tab ends what its shell started, not just the shell, and a shell
// that exits by itself ends its tab.
func TestConPTYHangupEndsProcessTree(t *testing.T) {
	useSettings(t, map[string]any{})
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.txt")
	sh, err := startShell(dir, "powershell.exe",
		"(Start-Process ping -ArgumentList '-n','120','127.0.0.1' -NoNewWindow -PassThru).Id | Set-Content -Encoding ascii child.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, func() bool {
		b, err := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && pid > 0
	})
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open child %d: %v", pid, err)
	}
	defer windows.CloseHandle(child)
	closeWithin(t, sh, 15*time.Second)
	if ev, _ := windows.WaitForSingleObject(child, 10_000); ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("child %d still runs after its tab closed", pid)
	}

	quits, err := startShell(t.TempDir(), "cmd.exe", "exit", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, quits.Exited)
	closeWithin(t, quits, 15*time.Second)
}
