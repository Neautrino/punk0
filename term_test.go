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
)

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
	sh, err := startShell(dir, "/bin/sh", "stty raw -echo && touch ready && head -c "+strconv.Itoa(len(want))+" > got", nil)
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
