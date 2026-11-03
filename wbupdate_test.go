package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"px0/internal/update"
)

func TestWbStateCarriesUpdateAndWhatsNew(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "punk0", nil)
	var s map[string]any
	getJSON(t, wb, "/api/wb/state", &s)
	if _, ok := s["update"]; ok {
		t.Fatalf("update without one installed: %v", s["update"])
	}
	if _, ok := s["whatsNew"]; ok {
		t.Fatalf("whatsNew without one pending: %v", s["whatsNew"])
	}
	wb.upd.setInstalled("9.9.9")
	wb.upd.setPendingNotes(version)
	s = nil
	getJSON(t, wb, "/api/wb/state", &s)
	if s["update"] != "9.9.9" || s["whatsNew"] != version {
		t.Fatalf("update = %v, whatsNew = %v", s["update"], s["whatsNew"])
	}
}

func TestWhatsNewSeenClearsPending(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "punk0", nil)
	wb.upd.setPendingNotes(version)

	cross := httptest.NewRequest("POST", "/api/wb/whats-new/seen", nil)
	cross.Host = "127.0.0.1:7777"
	cross.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, cross)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin seen: %d", w.Code)
	}
	if w := reqJSON(t, wb, "GET", "/api/wb/whats-new/seen", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET seen: %d", w.Code)
	}
	if wb.upd.pendingNotes() == "" {
		t.Fatal("a refused request cleared the notes")
	}
	if w := reqJSON(t, wb, "POST", "/api/wb/whats-new/seen", ""); w.Code != http.StatusOK {
		t.Fatalf("seen: %d %s", w.Code, w.Body.String())
	}
	if v := wb.upd.pendingNotes(); v != "" {
		t.Fatalf("still pending: %q", v)
	}
	if v := update.WhatsNewPending(version, false); v != "" {
		t.Fatalf("the next start would open the notes again: %q", v)
	}
}

func TestRestartRequestQueuesOnce(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "punk0", nil)
	if w := reqJSON(t, wb, "GET", "/api/wb/restart", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET restart: %d", w.Code)
	}
	cross := httptest.NewRequest("POST", "/api/wb/restart", nil)
	cross.Host = "127.0.0.1:7777"
	cross.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, cross)
	if w.Code != http.StatusForbidden || len(wb.upd.restart) != 0 {
		t.Fatalf("cross-origin restart: %d, queued %d", w.Code, len(wb.upd.restart))
	}
	// A second click while the first waits must neither block nor queue twice.
	for i := range 2 {
		if w := reqJSON(t, wb, "POST", "/api/wb/restart", ""); w.Code != http.StatusAccepted {
			t.Fatalf("restart %d: %d", i, w.Code)
		}
	}
	if n := len(wb.upd.restart); n != 1 {
		t.Fatalf("queued %d restarts, want 1", n)
	}
}

func TestWhatsNewServesSafeNotes(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "punk0", nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.9.1","body":"### Added\n- **Fonts.** <script>x</script>served"}`))
	}))
	defer srv.Close()
	t.Setenv("PUNK0_UPDATE_URL", srv.URL)

	if w := reqJSON(t, wb, "GET", "/api/wb/whats-new?v=../../etc", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad version: %d", w.Code)
	}
	var res struct{ Version, HTML, Release, Changelog string }
	getJSON(t, wb, "/api/wb/whats-new?v=0.9.1", &res)
	if res.Version != "0.9.1" || !strings.Contains(res.HTML, `id="added"`) || strings.Contains(res.HTML, "<script") {
		t.Fatalf("notes: %+v", res)
	}
	if !strings.HasSuffix(res.Release, "/releases/tag/v0.9.1") || !strings.HasSuffix(res.Changelog, "#v0.9.1") {
		t.Fatalf("links: %+v", res)
	}
}
