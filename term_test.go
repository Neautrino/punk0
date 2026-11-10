package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	xterm "github.com/gitpod-io/xterm-go"
)

// bufferText is every line a terminal holds, scrollback first, as a viewer
// shows it.
func bufferText(term *xterm.Terminal) string {
	b := term.Buffer()
	lines := make([]string, b.Lines.Length())
	for i := range lines {
		lines[i] = b.Lines.Get(i).TranslateToString(true, 0, -1)
	}
	return strings.Join(lines, "\n")
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
	sh, err := startShell(dir, "/bin/sh", "stty raw -echo && touch ready && head -c "+strconv.Itoa(len(want))+" > got", nil, nil)
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

// A stop by hand hangs the tab's shell up and keeps the tab, to start again
// when opened. One that would lose something is refused with what until it
// is forced.
func TestTabStop(t *testing.T) {
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
	if w := postJSON(t, wb, "/api/tabs/nope/stop", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown tab: %d %s", w.Code, w.Body.String())
	}
	tab, err := wb.records.AddTab(ws.ID, tabClaude, "")
	if err != nil {
		t.Fatal(err)
	}
	if w := postJSON(t, wb, "/api/tabs/"+tab.ID+"/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stopped tab: %d %s", w.Code, w.Body.String())
	}
	sh, err := startShell(t.TempDir(), "/bin/sh", "sleep 30", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tab.mu.Lock()
	tab.sh = sh
	tab.mu.Unlock()
	tab.setStatus(statusWorking)
	if w := postJSON(t, wb, "/api/tabs/"+tab.ID+"/stop", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "working") {
		t.Fatalf("working agent: %d %s", w.Code, w.Body.String())
	}
	if !tab.running() {
		t.Fatal("a refused stop stopped the tab")
	}
	if w := postJSON(t, wb, "/api/tabs/"+tab.ID+"/stop?force=1", ""); w.Code != http.StatusOK {
		t.Fatalf("forced stop: %d %s", w.Code, w.Body.String())
	}
	if tab.running() || tab.Status() != "" {
		t.Fatalf("after the stop: running %v, status %q", tab.running(), tab.Status())
	}
	if wb.records.Tab(tab.ID) == nil {
		t.Fatal("the stop removed the tab")
	}
}

func TestStopLoss(t *testing.T) {
	for _, c := range []struct {
		name, kind, status string
		foreground, resumes bool
		lost               bool
	}{
		{"shell at its prompt", tabShell, "", true, true, false},
		{"shell running a command", tabShell, "", false, true, true},
		{"idle agent", tabClaude, statusIdle, false, true, false},
		{"agent without status", "codex", "", false, true, false},
		{"working agent", tabClaude, statusWorking, false, true, true},
		{"agent waiting for an answer", tabClaude, statusWaiting, false, true, true},
		{"agent that cannot resume", "cursor-agent", "", false, false, true},
	} {
		if got := stopLoss(c.kind, c.status, c.foreground, c.resumes); (got != "") != c.lost {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

// An agent tab picks its chat up again on its next start unless its agent
// resumes only by id and the tab has none.
func TestTabResumes(t *testing.T) {
	for _, c := range []struct {
		kind, session string
		want          bool
	}{
		{tabShell, "", true},
		{tabClaude, "S", true},
		{"gemini", "", true},
		{"cursor-agent", "S", true},
		{"cursor-agent", "", false},
	} {
		if got := (&Satellite{Kind: c.kind, Session: c.session}).resumes(); got != c.want {
			t.Errorf("%s with session %q: %v, want %v", c.kind, c.session, got, c.want)
		}
	}
}

// agy says what it is doing only in its terminal (OSC 7501): these are agy
// 1.4.12's own reports, captured from a run that signed in, ran a tool behind
// a permission prompt and finished (hookspike inject/agy/osc.raw). The
// sign-in report fires at every launch, logged in or not, so it says nothing
// about the chat.
func TestAgyStatus(t *testing.T) {
	for _, c := range []struct {
		data string
		want string
		ok   bool
	}{
		{"state=blocked:kind=auth:app=antigravity:msg=U2lnbiBpbiByZXF1aXJlZA==", "", false},
		{"state=idle:app=antigravity", statusIdle, true},
		{"state=working:app=antigravity", statusWorking, true},
		{"state=blocked:kind=permission:app=antigravity:msg=QXBwcm92ZSBSdW5Db21tYW5kOiBlY2hvIGhpID4gaGVsbG8udHh0", statusWaiting, true},
		{"state=done:app=antigravity:msg=RE9ORQ==", statusIdle, true},
		// AGY_CLI_OSC7501_MESSAGES=brief drops the message.
		{"state=working:app=antigravity:msg=", statusWorking, true},
		// Not agy, or not a state punk0 knows.
		{"state=working:app=zed", "", false},
		{"state=blocked:app=antigravity", "", false},
		{"state=thinking:app=antigravity", "", false},
		{"state=working", "", false},
		{"", "", false},
	} {
		got, ok := agyStatus(c.data)
		if got != c.want || ok != c.ok {
			t.Errorf("agyStatus(%q) = %q, %v; want %q, %v", c.data, got, ok, c.want, c.ok)
		}
	}
}
