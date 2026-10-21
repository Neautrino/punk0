package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestAs(h http.Handler, host, path string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// A DNS-rebound page reads under its own domain; the browser's same-origin
// rule does not stop it, so the server must.
func TestLoopbackServerRefusesReboundReads(t *testing.T) {
	s, _ := newTestServer(t)
	s.localOnly = true
	const file = "/api/file?path=main.go"
	if code := requestAs(s, "attacker.example:7777", file); code != http.StatusForbidden {
		t.Fatalf("rebound read = %d, want 403", code)
	}
	for _, host := range []string{"127.0.0.1:7777", "localhost:7777", "[::1]:7777"} {
		if code := requestAs(s, host, file); code != http.StatusOK {
			t.Errorf("read via %s = %d, want 200", host, code)
		}
	}
}

// In remote mode the user chose to be reachable, maybe under a hostname.
func TestRemoteServerAcceptsHostnames(t *testing.T) {
	s, _ := newTestServer(t)
	if code := requestAs(s, "box.tailnet.ts.net:7777", "/api/file?path=main.go"); code != http.StatusOK {
		t.Fatalf("remote read via hostname = %d, want 200", code)
	}
}

func TestLoopbackWorkbenchRefusesReboundRequests(t *testing.T) {
	newTestStore(t)
	wb := newWorkbench("/", t.TempDir(), "punk0", nil)
	defer wb.Close()
	wb.localOnly = true
	if code := requestAs(wb, "attacker.example:7777", "/"); code != http.StatusForbidden {
		t.Fatalf("rebound workbench page = %d, want 403", code)
	}
	if code := requestAs(wb, "127.0.0.1:7777", "/"); code != http.StatusOK {
		t.Fatalf("workbench page via 127.0.0.1 = %d, want 200", code)
	}
}

func TestLoopbackBind(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true, "localhost": true, "::1": true, "[::1]": true, "127.0.0.2": true,
		"0.0.0.0": false, "::": false, "": false, "192.168.1.5": false, "box.tailnet.ts.net": false,
	} {
		if got := loopbackBind(addr); got != want {
			t.Errorf("loopbackBind(%q) = %v, want %v", addr, got, want)
		}
	}
}
