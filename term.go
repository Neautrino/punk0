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

// termHandler serves GET /term: one websocket drives one PTY running the
// user's shell in dir. Binary frames carry PTY bytes both ways; text frames
// carry {"cols":N,"rows":N}. The shell lives as long as the websocket.
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
			var size struct{ Cols, Rows uint16 }
			if json.Unmarshal(data, &size) == nil && size.Cols > 0 && size.Rows > 0 {
				setPTYSize(ptmx, size.Cols, size.Rows)
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
