//go:build windows

package main

import (
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultShell is PowerShell 7, else Windows PowerShell, else cmd.exe.
// $SHELL is not consulted: Git Bash sets it to a path only its own programs
// understand.
func defaultShell() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	return "cmd.exe"
}

// conTTY is a shell on a pseudo console (ConPTY, Windows 10 1809 and later).
// The shell runs in a job that kills what is left in it when hung up, so the
// agent and anything it started go with the tab, as a Unix hangup takes the
// terminal's process group.
type conTTY struct {
	pid     int
	process windows.Handle
	job     windows.Handle
	out     windows.Handle // our end of the console's output
	outOnce sync.Once

	inMu     sync.Mutex // guards in against Hangup closing it mid-Write
	in       windows.Handle
	inClosed bool

	consoleMu     sync.Mutex // guards console: a closed one is freed memory
	console       windows.Handle
	consoleClosed bool

	hangup sync.Once
}

func startTTY(argv []string, dir string, env []string) (tty, error) {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, err
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
		return nil, err
	}
	var console windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, inRead, outWrite, 0, &console)
	// The console keeps its own copies of its pipe ends. Dropping ours lets
	// a Read see the console go away.
	closeHandles(inRead, outWrite)
	if err != nil {
		closeHandles(inWrite, outRead)
		return nil, err
	}
	t := &conTTY{in: inWrite, out: outRead, console: console}
	if err := t.spawn(path, argv, dir, env); err != nil {
		windows.ClosePseudoConsole(console)
		closeHandles(inWrite, outRead)
		return nil, err
	}
	return t, nil
}

// spawn starts the shell on the console, suspended until it is in the job,
// so nothing it starts can slip out before.
func (t *conTTY) spawn(path string, argv []string, dir string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	// The attribute's value is the console handle itself, not a pointer to it.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		*(*unsafe.Pointer)(unsafe.Pointer(&t.console)), unsafe.Sizeof(t.console)); err != nil {
		return err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// Invalid std handles make the shell take the console's own (as
	// WezTerm's ConPTY code does); without them it would write to punk0's
	// stdio. Wine 10 does not do this substitution, so only programs that
	// open CONIN$/CONOUT$ themselves show output there.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput, si.StdOutput, si.StdErr = windows.InvalidHandle, windows.InvalidHandle, windows.InvalidHandle

	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(commandLine(argv))
	if err != nil {
		return err
	}
	var cwd *uint16
	if dir != "" {
		if cwd, err = windows.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}
	block, err := envBlock(env)
	if err != nil {
		return err
	}
	job, err := newKillJob()
	if err != nil {
		return err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(app, line, nil, nil, false, flags, block, cwd, &si.StartupInfo, &pi); err != nil {
		windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(pi.Thread)
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		closeHandles(pi.Process, job)
		return err
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		windows.TerminateJobObject(job, 1)
		closeHandles(pi.Process, job)
		return err
	}
	t.pid, t.process, t.job = int(pi.ProcessId), pi.Process, job
	return nil
}

// newKillJob is a job whose processes all end when it is closed.
func newKillJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// commandLine joins argv into one Windows command line. cmd.exe parses its
// own line instead of splitting it the usual way, so its arguments
// (shellSyntax.launch) go in verbatim.
func commandLine(argv []string) string {
	if syntaxOf(argv[0]) == cmdShell {
		return strings.Join(append([]string{windows.EscapeArg(argv[0])}, argv[1:]...), " ")
	}
	return windows.ComposeCommandLine(argv)
}

// envBlock is env as CreateProcess takes it. A later entry wins over an
// earlier one of the same name, as with exec.Cmd; names are case-insensitive
// on Windows, and those of the per-drive folders start with "=".
func envBlock(env []string) (*uint16, error) {
	seen := make(map[string]bool, len(env))
	keep := make([]string, 0, len(env))
	for _, kv := range slices.Backward(env) {
		if kv == "" {
			continue
		}
		name := kv
		if i := strings.IndexByte(kv[1:], '='); i >= 0 {
			name = kv[:i+1]
		}
		name = strings.ToUpper(name)
		if seen[name] {
			continue
		}
		seen[name] = true
		keep = append(keep, kv)
	}
	slices.Reverse(keep)
	var block []uint16
	for _, kv := range keep {
		s, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, err
		}
		block = append(block, s...)
	}
	block = append(block, 0)
	if len(keep) == 0 {
		block = append(block, 0)
	}
	return &block[0], nil
}

func (t *conTTY) Read(p []byte) (int, error) {
	var n uint32
	if err := windows.ReadFile(t.out, p, &n, nil); err != nil {
		// The console is gone (ERROR_BROKEN_PIPE): nothing more will come.
		t.outOnce.Do(func() { windows.CloseHandle(t.out) })
		return int(n), io.EOF
	}
	return int(n), nil
}

func (t *conTTY) Write(p []byte) (int, error) {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	if t.inClosed {
		return 0, os.ErrClosed
	}
	var n uint32
	err := windows.WriteFile(t.in, p, &n, nil)
	return int(n), err
}

func (t *conTTY) Resize(cols, rows uint16) error {
	t.consoleMu.Lock()
	defer t.consoleMu.Unlock()
	if t.consoleClosed {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(t.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (t *conTTY) Pid() int { return t.pid }

func (t *conTTY) Wait() {
	windows.WaitForSingleObject(t.process, windows.INFINITE)
	windows.CloseHandle(t.process)
}

// Hangup kills the job, then closes the console, which ends a pending Read
// and fails a Write blocked on a full pipe so the input can be closed too.
func (t *conTTY) Hangup() {
	t.hangup.Do(func() {
		windows.TerminateJobObject(t.job, 1)
		windows.CloseHandle(t.job)
		t.consoleMu.Lock()
		windows.ClosePseudoConsole(t.console)
		t.consoleClosed = true
		t.consoleMu.Unlock()
		t.inMu.Lock()
		windows.CloseHandle(t.in)
		t.inClosed = true
		t.inMu.Unlock()
	})
}

// Foreground is false: a console has no foreground process group to tell a
// shell at its prompt from one running a job, so an idle shell is never
// stopped on Windows.
func (t *conTTY) Foreground() bool { return false }

func closeHandles(hs ...windows.Handle) {
	for _, h := range hs {
		windows.CloseHandle(h)
	}
}

// setProcessGroup has no Windows equivalent: exec.CommandContext kills the
// process directly on cancel.
func setProcessGroup(cmd *exec.Cmd) {}
