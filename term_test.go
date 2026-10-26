package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

func TestTabSend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	_, ws, err := wb.records.Open(cleanRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	wb.localOnly = true
	useSettings(t, map[string]any{})
	t.Setenv("SHELL", "/bin/sh")
	tab, err := wb.records.AddTab(ws.ID, tabClaude, "")
	if err != nil {
		t.Fatal(err)
	}
	send := func(id, text string) (int, string) {
		body, _ := json.Marshal(map[string]string{"text": text})
		w := postJSON(t, wb, "/api/tabs/"+id+"/send", string(body))
		return w.Code, w.Body.String()
	}
	if code, body := send("nope", "hi"); code != http.StatusNotFound {
		t.Fatalf("unknown tab: %d %s", code, body)
	}
	if code, body := send(tab.ID, "hi"); code != http.StatusConflict || !strings.Contains(body, "chat isn't running") {
		t.Fatalf("stopped tab: %d %s", code, body)
	}

	// The tab's program records exactly what reaches it, in raw mode so the
	// terminal turns no \r into \n. The escape a text carries is dropped, so
	// it cannot end the paste.
	dir := t.TempDir()
	const want = "\x1b[200~fix it\nplease[201~\x1b[201~\r"
	sh, err := startShell(dir, "stty raw -echo && touch ready && head -c "+strconv.Itoa(len(want))+" > got", nil)
	if err != nil {
		t.Fatal(err)
	}
	tab.mu.Lock()
	tab.sh = sh
	tab.mu.Unlock()
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(dir, "ready")); return err == nil })

	for _, status := range []string{statusWorking, statusWaiting} {
		tab.setStatus(status)
		if code, body := send(tab.ID, "too early"); code != http.StatusConflict || !strings.Contains(body, "chat is busy") {
			t.Fatalf("%s tab: %d %s", status, code, body)
		}
	}
	tab.setStatus(statusIdle)
	if code, body := send(tab.ID, "fix it\r\nplease\x1b[201~"); code != http.StatusOK {
		t.Fatalf("idle tab: %d %s", code, body)
	}
	waitFor(t, func() bool { b, _ := os.ReadFile(filepath.Join(dir, "got")); return len(b) >= len(want) })
	if got, _ := os.ReadFile(filepath.Join(dir, "got")); string(got) != want {
		t.Fatalf("typed %q, want %q", got, want)
	}
}
