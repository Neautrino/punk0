//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	xterm "github.com/gitpod-io/xterm-go"
)

// A viewer that attaches late sees what scrolled by before it, under a
// screen that has since been redrawn in place many times over, as a TUI's
// is: the raw tail of the output would start among the redraws.
func TestAttachDrawsScrolledAwayOutput(t *testing.T) {
	useSettings(t, map[string]any{})
	const redraws = 30000 // about 480 KiB of cursor moves and frames
	cmd := `awk 'BEGIN { for (i = 1; i <= 60; i++) print "line " i; ` +
		`for (i = 1; i <= ` + strconv.Itoa(redraws) + `; i++) printf "\033[24;1Hframe %05d", i; print ""; print "end" }'`
	sh, err := startShell(t.TempDir(), "/bin/sh", cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sh.Close)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		sh.mu.Lock()
		done := strings.Contains(bufferText(sh.screen), "\nend")
		sh.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the command never finished")
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		sh.Attach(r.Context(), conn, nil)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(-1)

	typ, data, err := c.Read(ctx)
	var size struct{ Cols, Rows int }
	if err != nil || typ != websocket.MessageText || json.Unmarshal(data, &size) != nil || size.Cols != ttyCols || size.Rows != ttyRows {
		t.Fatalf("first frame %v %q %v, want the screen's size %dx%d", typ, data, err, ttyCols, ttyRows)
	}
	typ, data, err = c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary {
		t.Fatalf("second frame %v %v, want the screen", typ, err)
	}
	viewer := xterm.New(xterm.WithCols(size.Cols), xterm.WithRows(size.Rows), xterm.WithScrollback(scrollbackLines()))
	viewer.Write(data)
	got := bufferText(viewer)
	for _, want := range []string{"line 1\n", "line 60\n", "frame " + strconv.Itoa(redraws), "\nend"} {
		if !strings.Contains(got, want) {
			t.Errorf("the attached viewer shows no %q; it shows:\n%s", want, got)
		}
	}
}

// One tab can be shown by the agents page and the editor at once; its PTY
// has one size, which follows the viewer that reported last and falls back to
// the next most recent one when that viewer leaves.
func TestShellSizeFollowsLatestViewer(t *testing.T) {
	useSettings(t, map[string]any{})
	sh, err := startShell(t.TempDir(), "/bin/sh", "", nil)
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
		sh.Attach(r.Context(), conn, nil)
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
			if ws, err := pty.GetsizeFull(sh.tty.(*unixTTY).f); err == nil {
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
