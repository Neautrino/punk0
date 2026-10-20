package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// maxTermFrame bounds one browser->PTY frame; a paste arrives as a single
// frame and the library default (32 KiB) would drop the connection.
const maxTermFrame = 8 << 20

// scrollback is how much output a shell keeps for viewers that attach later:
// switching satellites or reloading the page replays it.
const scrollback = 256 << 10

// viewerQueue is how many PTY reads a viewer may lag behind. A slower viewer
// is disconnected with StatusTryAgainLater and reattaches from scrollback,
// rather than silently missing bytes mid escape sequence.
const viewerQueue = 256

// claudeCommand is typed into the shell by the Claude button. The server owns
// it so session ids and hook settings can be added here later.
const claudeCommand = "claude"

// Shell is a satellite's shell in a PTY. It outlives websockets: viewers
// attach and detach while the shell and any agent in it keep running.
type Shell struct {
	cmd  *exec.Cmd
	ptmx *os.File
	stop context.CancelFunc
	done chan struct{} // closed once the shell is reaped and output drained

	mu      sync.Mutex // guards out, viewers, exited
	out     ring
	viewers map[chan []byte]struct{}
	exited  bool
}

func startShell(dir string) (*Shell, error) {
	ctx, stop := context.WithCancel(context.Background())
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
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

// input writes browser keystrokes into the PTY and applies resize and
// launcher messages until the websocket closes.
func (s *Shell) input(ctx context.Context, conn *websocket.Conn) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := s.ptmx.Write(data); err != nil {
				return
			}
			continue
		}
		var msg struct {
			Cols, Rows uint16
			Run        string
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Cols > 0 && msg.Rows > 0 {
			setPTYSize(s.ptmx, msg.Cols, msg.Rows)
		}
		// Only type into the shell while it sits at its prompt (it is the
		// PTY's foreground process group), never into a running program.
		if msg.Run == "claude" {
			if fg, err := ptyForeground(s.ptmx); err == nil && fg == s.cmd.Process.Pid {
				s.ptmx.Write([]byte("\x15" + claudeCommand + "\r")) // ^U clears a half-typed line
			}
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

// handleTerm serves GET /api/satellites/{id}/term. Binary frames carry PTY
// bytes both ways; text frames carry {"cols":N,"rows":N} or {"run":"claude"}.
func handleTerm(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sat := satelliteOf(reg, w, r)
		if sat == nil {
			return
		}
		// nil options: Accept rejects handshakes whose Origin differs from Host.
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		sh, err := sat.Shell()
		if err != nil {
			log.Printf("term %s: %v", sat.ID, err)
			conn.Close(websocket.StatusInternalError, err.Error())
			return
		}
		sh.Attach(r.Context(), conn)
	}
}
