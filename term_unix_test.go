//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	sh, err := startShell(t.TempDir(), "/bin/sh", cmd, nil, nil)
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
	sh, err := startShell(t.TempDir(), "/bin/sh", "", nil, nil)
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

// What an agent prints about itself reaches the shell's owner while the shell
// keeps running, in the order it was printed.
func TestShellReportsAgentStatus(t *testing.T) {
	useSettings(t, map[string]any{})
	dir := t.TempDir()
	got := make(chan string, 8)
	cmd := `printf '\033]7501;state=blocked:kind=auth:app=antigravity:msg=U2lnbiBpbiByZXF1aXJlZA==\007'; ` +
		`printf '\033]7501;state=working:app=antigravity\007'; ` +
		`while [ ! -f go ]; do sleep 0.05; done; ` +
		`printf '\033]7501;state=idle:app=antigravity\007'; sleep 60`
	sh, err := startShell(dir, "/bin/sh", cmd, nil, func(_ *Shell, status string) { got <- status })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sh.Close)
	next := func() string {
		t.Helper()
		select {
		case s := <-got:
			return s
		case <-time.After(10 * time.Second):
			t.Fatal("the shell reported no status")
			return ""
		}
	}
	// The sign-in report says nothing, so the first status is the next one.
	if s := next(); s != statusWorking {
		t.Fatalf("first status %q, want %q", s, statusWorking)
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if s := next(); s != statusIdle {
		t.Fatalf("second status %q, want %q", s, statusIdle)
	}
}

// Closing a shell never waits on whoever is being told about it: a tab is
// closed while holding its own lock, which is the lock a status report takes.
func TestShellCloseWhileReportingStatus(t *testing.T) {
	useSettings(t, map[string]any{})
	reporting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	report := func(*Shell, string) {
		once.Do(func() { close(reporting) })
		<-release
	}
	defer close(release)
	sh, err := startShell(t.TempDir(), "/bin/sh",
		`printf '\033]7501;state=working:app=antigravity\007'; sleep 60`, nil, report)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-reporting:
	case <-time.After(10 * time.Second):
		t.Fatal("the shell reported no status")
	}
	closed := make(chan struct{})
	go func() {
		sh.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close waited for the status report to finish")
	}
}

// An agy tab's dot follows what agy prints in its terminal, which is the only
// thing agy tells punk0: punk0 starts it with the switch that makes it report
// (AGY_CLI_OSC7501), the permission prompt shows as needs-you, and DONE as
// idle. The same reports in a tab that is not running agy say nothing.
func TestAgyTabStatusFromItsTerminal(t *testing.T) {
	newTestStore(t)
	useSettings(t, map[string]any{})
	t.Setenv("SHELL", "/bin/sh") // a shell punk0 cannot hook: the tab's own environment carries the switch
	bin := t.TempDir()
	writeExec(t, filepath.Join(bin, "agy"), "#!/bin/sh\n"+
		"echo \"osc=$AGY_CLI_OSC7501\" > osc-env\n"+
		"printf '\\033]7501;state=blocked:kind=auth:app=antigravity:msg=U2lnbiBpbiByZXF1aXJlZA==\\007'\n"+
		"printf '\\033]7501;state=working:app=antigravity\\007'\n"+
		"while [ ! -f ask ]; do sleep 0.05; done\n"+
		"printf '\\033]7501;state=blocked:kind=permission:app=antigravity:msg=QXBwcm92ZQ==\\007'\n"+
		"while [ ! -f stop ]; do sleep 0.05; done\n"+
		"printf '\\033]7501;state=done:app=antigravity:msg=RE9ORQ==\\007'\n"+
		"echo agy-finished\n"+
		"sleep 60\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	_, ws, err := wb.records.Open(cleanRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := wb.records.AddTab(ws.ID, tabAgy, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tab.Shell(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tab.close)
	step := func(status string) {
		t.Helper()
		waitFor(t, func() bool { return tab.Status() == status })
	}
	step(statusWorking) // the sign-in report before it left the tab alone
	if got, _ := os.ReadFile(filepath.Join(tab.dir, "osc-env")); strings.TrimSpace(string(got)) != "osc=on" {
		t.Fatalf("punk0 started agy with %q, want osc=on", got)
	}
	for _, f := range []struct{ name, status string }{{"ask", statusWaiting}, {"stop", statusIdle}} {
		if err := os.WriteFile(filepath.Join(tab.dir, f.name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		step(f.status)
	}

	// A shell tab running the same program is still a shell: nothing binds
	// it to agy, so its reports are not its tab's.
	shell, err := wb.records.AddTab(ws.ID, tabShell, "")
	if err != nil {
		t.Fatal(err)
	}
	sh, _, err := shell.Shell()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.close)
	waitShown(t, sh, "$ ") // typing before the shell is at its prompt is lost
	sh.tty.Write([]byte("agy\r"))
	waitShown(t, sh, "agy-finished")
	if got := shell.Status(); got != "" {
		t.Fatalf("shell tab took agy's status %q", got)
	}
	if got := tab.Status(); got != statusIdle {
		t.Fatalf("agy tab's status became %q after another tab's agy ran", got)
	}
}
