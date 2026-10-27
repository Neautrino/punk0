package main

// Terminal tabs: a shell, or an agent in one, running in a PTY the browser
// attaches to over a websocket. Shells outlive the page: closing a browser
// tab, or the whole browser, leaves the agent running until punk0 stops.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/coder/websocket"

	"px0/internal/settings"
	"px0/internal/ui"
)

// maxTermFrame bounds one browser->PTY frame; a paste arrives as a single
// frame and the library default (32 KiB) would drop the connection.
const maxTermFrame = 8 << 20

// scrollback is how much output a shell keeps for viewers that attach later:
// switching tabs or reloading the page replays it.
const scrollback = 256 << 10

// viewerQueue is how many PTY reads a viewer may lag behind. A slower viewer
// is disconnected with StatusTryAgainLater and reattaches from scrollback,
// rather than silently missing bytes mid escape sequence.
const viewerQueue = 256

// claudeCommand starts Claude Code in a claude tab's new shell, with the
// hook settings punk0 writes (hook.go) so the tab can show what the agent is
// doing. bin is the program with any user arguments.
func claudeCommand(bin string, sx shellSyntax) string {
	return bin + " --settings " + sx.quote(claudeHooksPath())
}

// tty is a shell on a terminal: a PTY on Unix (term_unix.go), a pseudo
// console on Windows (term_windows.go).
type tty interface {
	io.ReadWriter
	Resize(cols, rows uint16) error
	Pid() int
	// Wait returns once the shell has exited and been reaped.
	Wait()
	// Hangup ends the shell and what it started, and closes the terminal so
	// a pending Read returns, even if a stray job still holds it open. It
	// may be called again, and after the shell has exited.
	Hangup()
	// Foreground reports whether the shell itself is what the terminal is
	// talking to: no job runs in front of it.
	Foreground() bool
}

// Shell is a tab's shell in a PTY. It outlives websockets: viewers attach
// and detach while the shell and any agent in it keep running.
type Shell struct {
	tty  tty
	done chan struct{} // closed once the shell is reaped and output drained

	// lastIO is when the terminal last carried anything, in unix
	// nanoseconds; the idle reaper stops a tab nobody and nothing has used.
	lastIO atomic.Int64
	// closed is set by Close: punk0 stopped the shell, it did not exit on
	// its own.
	closed atomic.Bool

	mu      sync.Mutex // guards out, viewers, sizes, exited
	out     ring
	viewers map[chan []byte]struct{}
	// sizes is each viewer's last reported size, least recently reported
	// first. One PTY has one size, so the viewer that reported last (a client
	// reports again when focused) gets it, as tmux's window-size latest does;
	// when that viewer detaches, the next most recent one's size comes back.
	sizes  []viewerSize
	exited bool
}

type viewerSize struct {
	v          chan []byte
	cols, rows uint16
}

// shellSyntax is how a tab's shell is told to run a command and then stay
// open at its prompt.
type shellSyntax int

const (
	posixShell shellSyntax = iota // sh, bash, zsh, fish; Git Bash on Windows
	powerShell                    // pwsh, and Windows PowerShell
	cmdShell                      // cmd.exe
)

// syntaxOf tells a shell's syntax by its program name, a Windows path or not.
func syntaxOf(shell string) shellSyntax {
	switch strings.TrimSuffix(strings.ToLower(shell[strings.LastIndexAny(shell, `/\`)+1:]), ".exe") {
	case "pwsh", "powershell":
		return powerShell
	case "cmd":
		return cmdShell
	}
	return posixShell
}

// quote makes s one word of a command line in this syntax.
func (sx shellSyntax) quote(s string) string {
	switch sx {
	case powerShell:
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	case cmdShell:
		return `"` + s + `"` // what cmd.exe quotes in can hold no "
	}
	return shellQuote(s)
}

// orElse runs a, and b only when a fails.
func (sx shellSyntax) orElse(a, b string) string {
	if sx == powerShell {
		// Windows PowerShell 5.1 has no ||.
		return a + "; if ($LASTEXITCODE) { " + b + " }"
	}
	return a + " || " + b
}

// launch is the shell's arguments to run command and then stay open, as if
// the command had been typed at its prompt. A POSIX shell runs it
// interactively, so the rc files set PATH, and then replaces itself with a
// plain one. cmd.exe reads its command line itself: with /S the quotes around
// command are all it strips, so its arguments go verbatim (term_windows.go).
func (sx shellSyntax) launch(command string) []string {
	switch sx {
	case powerShell:
		return []string{"-NoLogo", "-NoExit", "-Command", command}
	case cmdShell:
		return []string{"/S", "/K", `"` + command + `"`}
	}
	return []string{"-i", "-c", command + `; exec "$SHELL"`}
}

// terminalShell is the program a new tab runs: terminal.shell, else the
// system's default (defaultShell).
func terminalShell() string {
	if shell := settings.SettingOr[string]("terminal.shell"); shell != "" {
		return shell
	}
	return defaultShell()
}

// tabProcess is the arguments and environment startShell runs shell with.
// terminal.env comes after the inherited environment so it wins there, and
// before env so punk0's own variables (hook URL, tab id) still win over the
// user's. A terminal.env that does not parse is ignored rather than failing
// the tab; saving one is refused. A POSIX shell gets SHELL, which its launch
// execs; elsewhere SHELL keeps what tools that read it expect.
func tabProcess(shell, command string, env []string) (args []string, cmdEnv []string) {
	sx := syntaxOf(shell)
	if command != "" {
		args = sx.launch(command)
	}
	cmdEnv = os.Environ()
	if sx == posixShell {
		cmdEnv = append(cmdEnv, "SHELL="+shell)
	}
	cmdEnv = append(cmdEnv, "TERM=xterm-256color", "COLORTERM=truecolor")
	if user, err := settings.ParseEnvLines(settings.SettingOr[string]("terminal.env")); err == nil {
		cmdEnv = append(cmdEnv, user...)
	}
	return args, append(cmdEnv, env...)
}

// startShell runs shell interactively in dir, with env added to its
// environment. A non-empty command, written in the shell's syntax, runs
// first; quitting it leaves the shell's prompt.
func startShell(dir, shell, command string, env []string) (*Shell, error) {
	// Starting reports a missing working directory as a bare chdir failure;
	// checking first says which folder is gone.
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	args, cmdEnv := tabProcess(shell, command, env)
	t, err := startTTY(append([]string{shell}, args...), dir, cmdEnv)
	if err != nil {
		return nil, err
	}
	s := &Shell{
		tty:     t,
		done:    make(chan struct{}),
		out:     newRing(scrollback),
		viewers: map[chan []byte]struct{}{},
	}
	s.touch()
	go s.pump()
	return s, nil
}

// pump copies PTY output into scrollback and every viewer until the shell
// exits or is closed.
func (s *Shell) pump() {
	reaped := make(chan struct{})
	go func() {
		s.tty.Wait()
		// A background job can keep the PTY open after the shell exits;
		// hanging up ends the Read below.
		s.tty.Hangup()
		close(reaped)
	}()

	buf := make([]byte, 32<<10)
	for {
		n, err := s.tty.Read(buf)
		if n > 0 {
			s.touch()
			p := append([]byte(nil), buf[:n]...) // viewers hold p past the next Read
			s.mu.Lock()
			s.out.Write(p)
			for v := range s.viewers {
				select {
				case v <- p:
				default:
					s.dropLocked(v)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}

	s.mu.Lock()
	s.exited = true
	for v := range s.viewers {
		s.dropLocked(v)
	}
	s.mu.Unlock()
	s.tty.Hangup()
	<-reaped
	close(s.done)
}

// Exited reports whether the shell has ended; a new attach then needs a new shell.
func (s *Shell) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// Close hangs up the shell and waits until it is reaped.
func (s *Shell) Close() {
	s.closed.Store(true)
	s.tty.Hangup()
	<-s.done
}

// exitedByItself waits until the shell ends and reports whether it ended on
// its own (exit typed, the process killed) rather than through Close.
func (s *Shell) exitedByItself() bool {
	<-s.done
	return !s.closed.Load()
}

func (s *Shell) touch() { s.lastIO.Store(time.Now().UnixNano()) }

// IdleFor is how long nothing has been typed into or printed by the terminal.
func (s *Shell) IdleFor() time.Duration {
	return time.Since(time.Unix(0, s.lastIO.Load()))
}

// Pid is the shell's process id; whatever runs in the tab is below it.
func (s *Shell) Pid() int { return s.tty.Pid() }

// Foreground reports whether the shell itself is what the terminal is talking
// to: no job is running in front of it, so hanging it up loses no work. Only
// Unix knows; elsewhere an idle shell is never stopped.
func (s *Shell) Foreground() bool { return s.tty.Foreground() }

// Attach streams the shell to one websocket until either side ends. Taking
// the scrollback and registering as a viewer under one lock means no output
// is lost or repeated between the replay and the live stream.
func (s *Shell) Attach(ctx context.Context, conn *websocket.Conn) {
	conn.SetReadLimit(maxTermFrame)
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "shell exited")
		return
	}
	backlog := s.out.Bytes()
	v := make(chan []byte, viewerQueue)
	s.viewers[v] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.dropLocked(v)
		s.forgetSizeLocked(v)
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer cancel()
		s.input(ctx, conn, v)
	}()

	if len(backlog) > 0 && conn.Write(ctx, websocket.MessageBinary, backlog) != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-v:
			if !ok {
				if s.Exited() {
					conn.Close(websocket.StatusNormalClosure, "shell exited")
				} else {
					conn.Close(websocket.StatusTryAgainLater, "viewer fell behind")
				}
				return
			}
			if conn.Write(ctx, websocket.MessageBinary, p) != nil {
				return
			}
		}
	}
}

// input writes browser keystrokes into the PTY and applies viewer v's resizes
// until the websocket closes.
func (s *Shell) input(ctx context.Context, conn *websocket.Conn, v chan []byte) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			s.touch()
			if _, err := s.tty.Write(data); err != nil {
				return
			}
			continue
		}
		var msg struct{ Cols, Rows uint16 }
		if json.Unmarshal(data, &msg) == nil && msg.Cols > 0 && msg.Rows > 0 {
			s.mu.Lock()
			if _, live := s.viewers[v]; live { // a detached viewer's size must not come back
				s.sizes = append(slices.DeleteFunc(s.sizes, func(z viewerSize) bool { return z.v == v }),
					viewerSize{v, msg.Cols, msg.Rows})
				s.tty.Resize(msg.Cols, msg.Rows)
			}
			s.mu.Unlock()
		}
	}
}

// forgetSizeLocked drops detaching viewer v's size; if v had the PTY's size,
// the most recent remaining viewer's size is applied, so a narrow viewer that
// closes does not leave the others drawing at its width.
func (s *Shell) forgetSizeLocked(v chan []byte) {
	n := len(s.sizes)
	if n == 0 {
		return
	}
	wasCurrent := s.sizes[n-1].v == v
	s.sizes = slices.DeleteFunc(s.sizes, func(z viewerSize) bool { return z.v == v })
	if wasCurrent && len(s.sizes) > 0 && !s.exited {
		z := s.sizes[len(s.sizes)-1]
		s.tty.Resize(z.cols, z.rows)
	}
}

// pasteSettle is how long Send waits between a paste and the Enter that
// submits it: an agent's TUI that reads both at once may take the Enter as
// part of the paste, leaving the prompt typed but not sent.
const pasteSettle = 200 * time.Millisecond

// Send types text into the terminal as one bracketed paste, then presses
// Enter: what a user pasting a prompt and submitting it does. Control
// characters other than newline and tab are dropped, so the text cannot end
// the paste early and type keys of its own.
func (s *Shell) Send(text string) error {
	s.touch()
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, strings.ReplaceAll(text, "\r\n", "\n"))
	if _, err := s.tty.Write([]byte("\x1b[200~" + text + "\x1b[201~")); err != nil {
		return err
	}
	time.Sleep(pasteSettle)
	_, err := s.tty.Write([]byte{'\r'})
	return err
}

// dropLocked unregisters viewer v; closing its channel ends its Attach.
func (s *Shell) dropLocked(v chan []byte) {
	if _, ok := s.viewers[v]; ok {
		delete(s.viewers, v)
		close(v)
	}
}

// handleTerm serves GET /api/tabs/{id}/term. Binary frames carry PTY bytes
// both ways; text frames carry {"cols":N,"rows":N}. A terminal is a shell on
// this machine, so it is served only while punk0 listens on loopback.
func (wb *workbench) handleTerm(w http.ResponseWriter, r *http.Request) {
	if !wb.localOnly {
		fail(w, http.StatusForbidden, "terminals are served only when punk0 listens on this machine")
		return
	}
	tab := wb.records.Tab(r.PathValue("id"))
	if tab == nil {
		fail(w, http.StatusNotFound, "tab not found")
		return
	}
	// nil options: Accept rejects handshakes whose Origin differs from Host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	sh, started, err := tab.Shell()
	if err != nil {
		ui.Event("warn", tab.Kind, wb.records.TabLabel(tab.ID), "could not start: "+err.Error(), os.Stderr)
		slog.Warn("tab could not start", "tab", tab.ID, "kind", tab.Kind, "name", wb.records.TabLabel(tab.ID), "shell", terminalShell(), "err", err)
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	if started {
		// Named now: the tab may be gone by the time its shell exits.
		label := wb.records.TabLabel(tab.ID)
		ui.Event("start", tab.Kind, label, "", os.Stdout)
		slog.Info("tab started", "tab", tab.ID, "kind", tab.Kind, "name", label, "shell", terminalShell(), "pid", sh.Pid())
		at := time.Now()
		go func() {
			if sh.exitedByItself() {
				ui.Event("stop", tab.Kind, label, "exited", os.Stdout)
				slog.Info("tab exited", "tab", tab.ID, "kind", tab.Kind, "name", label, "ran", time.Since(at).Round(time.Second).String())
			}
		}()
	}
	sh.Attach(r.Context(), conn)
}

// handleTabSend serves POST /api/tabs/{id}/send {"text"}: the text goes to
// the tab's agent as a pasted prompt (Send). An agent in the middle of a turn
// or waiting on a question would take it as something else, so a busy one is
// refused, as is a tab with no shell. Like the terminal itself, it is served
// only while punk0 listens on loopback.
func (wb *workbench) handleTabSend(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	if !wb.localOnly {
		fail(w, http.StatusForbidden, "terminals are served only when punk0 listens on this machine")
		return
	}
	tab := wb.records.Tab(r.PathValue("id"))
	if tab == nil {
		fail(w, http.StatusNotFound, "tab not found")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if readJSONBody(r, &body) != nil || strings.TrimSpace(body.Text) == "" {
		fail(w, http.StatusBadRequest, "text is required")
		return
	}
	sh := tab.shell()
	if sh == nil {
		fail(w, http.StatusConflict, "chat isn't running")
		return
	}
	switch tab.Status() {
	case statusWorking, statusWaiting:
		fail(w, http.StatusConflict, "chat is busy")
		return
	}
	if err := sh.Send(body.Text); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
