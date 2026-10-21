package main

// Terminal tabs: a shell, or an agent in one, running in a PTY the browser
// attaches to over a websocket. Shells outlive the page: closing a browser
// tab, or the whole browser, leaves the agent running until punk0 stops.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
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
// doing.
func claudeCommand() string {
	return "claude --settings " + shellQuote(filepath.Join(workbenchStateDir(), "claude-hooks.json"))
}

// Shell is a tab's shell in a PTY. It outlives websockets: viewers attach
// and detach while the shell and any agent in it keep running.
type Shell struct {
	cmd  *exec.Cmd
	ptmx *os.File
	stop context.CancelFunc
	done chan struct{} // closed once the shell is reaped and output drained

	// lastIO is when the terminal last carried anything, in unix
	// nanoseconds; the idle reaper stops a tab nobody and nothing has used.
	lastIO atomic.Int64

	mu      sync.Mutex // guards out, viewers, exited
	out     ring
	viewers map[chan []byte]struct{}
	exited  bool
}

// startShell runs the user's interactive shell in dir, with env added to its
// environment. A non-empty command runs first, inside an interactive shell so
// the rc files set PATH, and the shell then replaces itself with a plain one:
// quitting the command leaves a prompt, as if it had been typed there.
func startShell(dir, command string, env []string) (*Shell, error) {
	// exec reports a missing working directory as a bare chdir failure after
	// the fork; checking first says which folder is gone.
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell)
	if command != "" {
		cmd = exec.CommandContext(ctx, shell, "-i", "-c", command+`; exec "$SHELL"`)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SHELL="+shell, "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Env = append(cmd.Env, env...)
	hangupOnCancel(cmd)
	cmd.WaitDelay = 2 * time.Second // then Wait SIGKILLs a shell that ignored the hangup
	ptmx, err := startPTY(cmd)
	if err != nil {
		stop()
		return nil, err
	}
	// Cancelling ctx hangs up the shell and closes the PTY, which ends pump's
	// Read even if a stray background job still holds the PTY open.
	context.AfterFunc(ctx, func() { ptmx.Close() })

	s := &Shell{
		cmd:     cmd,
		ptmx:    ptmx,
		stop:    stop,
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
		s.cmd.Wait()
		// A background job can keep the PTY open after the shell exits;
		// closing it ends the Read below.
		s.stop()
		close(reaped)
	}()

	buf := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(buf)
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
	s.stop()
	<-reaped
	close(s.done)
}

// Exited reports whether the shell has ended; a new attach then needs a new shell.
func (s *Shell) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// Close hangs up the shell's process group and waits until it is reaped.
func (s *Shell) Close() {
	s.stop()
	<-s.done
}

func (s *Shell) touch() { s.lastIO.Store(time.Now().UnixNano()) }

// IdleFor is how long nothing has been typed into or printed by the terminal.
func (s *Shell) IdleFor() time.Duration {
	return time.Since(time.Unix(0, s.lastIO.Load()))
}

// Pid is the shell's process id; whatever runs in the tab is below it.
func (s *Shell) Pid() int { return s.cmd.Process.Pid }

// Foreground reports whether the shell itself is what the terminal is talking
// to: no job is running in front of it, so hanging it up loses no work. Only
// Unix knows; elsewhere an idle shell is never stopped.
func (s *Shell) Foreground() bool {
	if s.cmd.Process == nil {
		return false
	}
	pgrp, err := foregroundPgrp(s.ptmx)
	return err == nil && pgrp == s.cmd.Process.Pid
}

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
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer cancel()
		s.input(ctx, conn)
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

// input writes browser keystrokes into the PTY and applies resizes until the
// websocket closes.
func (s *Shell) input(ctx context.Context, conn *websocket.Conn) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			s.touch()
			if _, err := s.ptmx.Write(data); err != nil {
				return
			}
			continue
		}
		var msg struct{ Cols, Rows uint16 }
		if json.Unmarshal(data, &msg) == nil && msg.Cols > 0 && msg.Rows > 0 {
			setPTYSize(s.ptmx, msg.Cols, msg.Rows)
		}
	}
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
	sh, err := tab.Shell()
	if err != nil {
		uiStatus("warn", "terminal "+tab.ID, err.Error(), 0, os.Stderr)
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	sh.Attach(r.Context(), conn)
}
