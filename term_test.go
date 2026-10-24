package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// One tab can be shown by the agents page and the editor at once; its PTY
// has one size, which follows the viewer that reported last and falls back to
// the next most recent one when that viewer leaves.
func TestShellSizeFollowsLatestViewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY size is read with TIOCGWINSZ")
	}
	t.Setenv("SHELL", "/bin/sh")
	useSettings(t, map[string]any{})
	sh, err := startShell(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sh.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		sh.Attach(r.Context(), conn)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	attach := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		go func() { // drain output so the viewer never falls behind
			for {
				if _, _, err := c.Read(ctx); err != nil {
					return
				}
			}
		}()
		return c
	}
	report := func(c *websocket.Conn, size string) {
		if err := c.Write(ctx, websocket.MessageText, []byte(size)); err != nil {
			t.Fatal(err)
		}
	}
	waitCols := func(step string, want uint16) {
		t.Helper()
		var got uint16
		for range 100 {
			if ws, err := pty.GetsizeFull(sh.ptmx); err == nil {
				if got = ws.Cols; got == want {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s: PTY has %d columns, want %d", step, got, want)
	}

	wide, narrow := attach(), attach()
	report(wide, `{"cols":200,"rows":50}`)
	waitCols("wide reports", 200)
	report(narrow, `{"cols":50,"rows":20}`)
	waitCols("narrow reports last", 50)
	report(wide, `{"cols":200,"rows":50}`)
	waitCols("wide is used again", 200)
	report(narrow, `{"cols":50,"rows":20}`)
	waitCols("narrow is used again", 50)

	narrow.Close(websocket.StatusNormalClosure, "")
	waitCols("narrow, the current viewer, leaves", 200)

	other := attach()
	report(other, `{"cols":120,"rows":40}`)
	waitCols("third viewer reports", 120)
	wide.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond) // a viewer that is not current leaving changes nothing
	waitCols("non-current viewer leaves", 120)
}
