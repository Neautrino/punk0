package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/coder/websocket"
)

// maxTermFrame bounds one browser->PTY frame; a paste arrives as a single
// frame and the library default (32 KiB) would drop the connection.
const maxTermFrame = 8 << 20

// claudeCommand is typed into the shell by the Claude button. The server owns
// it so session ids and hook settings can be added here later.
const claudeCommand = "claude"

// termHandler serves GET /term: one websocket drives one PTY running the
// user's shell in dir. Binary frames carry PTY bytes both ways; text frames
// carry {"cols":N,"rows":N} or {"run":"claude"}. The shell lives as long as
// the websocket.
type termHandler struct {
	dir string
}

func (h *termHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// nil options: Accept rejects handshakes whose Origin differs from Host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxTermFrame)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = h.dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	hangupOnCancel(cmd)
	cmd.WaitDelay = 2 * time.Second // then Wait SIGKILLs a shell that ignored the hangup
	ptmx, err := startPTY(cmd)
	if err != nil {
		log.Printf("term: %v", err)
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	// Cancelling ctx hangs up the shell and closes the PTY, which ends the
	// read loop below even if a stray background job still holds the PTY open.
	context.AfterFunc(ctx, func() { ptmx.Close() })

	// Browser -> PTY.
	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if _, err := ptmx.Write(data); err != nil {
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
				setPTYSize(ptmx, msg.Cols, msg.Rows)
			}
			// Only type into the shell while it sits at its prompt (it is the
			// PTY's foreground process group), never into a running program.
			if msg.Run == "claude" {
				if fg, err := ptyForeground(ptmx); err == nil && fg == cmd.Process.Pid {
					ptmx.Write([]byte("\x15" + claudeCommand + "\r")) // ^U clears a half-typed line
				}
			}
		}
	}()

	// PTY -> browser, until the shell exits.
	buf := make([]byte, 32<<10)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 && conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
			break
		}
		if err != nil {
			break
		}
	}
	cancel()
	cmd.Wait()
	conn.Close(websocket.StatusNormalClosure, "shell exited")
}
