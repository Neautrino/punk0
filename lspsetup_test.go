package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Only a POST from px0's own page, addressed by IP or localhost, may install.
func TestLocalPost(t *testing.T) {
	cases := []struct {
		name, method, host, origin string
		want                       bool
	}{
		{"same origin", "POST", "127.0.0.1:7777", "http://127.0.0.1:7777", true},
		{"localhost", "POST", "localhost:7777", "http://localhost:7777", true},
		{"ipv6 loopback", "POST", "[::1]:7777", "http://[::1]:7777", true},
		{"GET", "GET", "127.0.0.1:7777", "http://127.0.0.1:7777", false},
		{"no origin", "POST", "127.0.0.1:7777", "", false},
		{"other site", "POST", "127.0.0.1:7777", "https://evil.example", false},
		{"dns rebinding", "POST", "evil.example:7777", "http://evil.example:7777", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "/api/lsp/install", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := localPost(httptest.NewRecorder(), r); got != c.want {
			t.Errorf("%s: localPost = %v, want %v", c.name, got, c.want)
		}
	}
}

// A binary outside PATH is still found in one of the extra folders.
func TestLookPathInExtraDir(t *testing.T) {
	dir := t.TempDir()
	name := "px0-test-fake-language-server"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookPathIn(name, nil); ok {
		t.Fatalf("%s found without its folder; the test binary name collides with something on PATH", name)
	}
	if p, ok := lookPathIn(name, []string{t.TempDir(), dir}); !ok || filepath.Dir(p) != dir {
		t.Errorf("lookPathIn = %q, %v; want a path in %s", p, ok, dir)
	}
}

// Every recipe is well formed, and nothing px0 runs by itself asks for root.
func TestInstallRecipes(t *testing.T) {
	for _, def := range lspRegistry {
		if def.Lang == "" {
			t.Errorf("%s: no Lang", def.Name)
		}
		for _, in := range def.Install {
			if len(in.Cmd) < 2 {
				t.Errorf("%s: install command %q too short", def.Name, in.Cmd)
			}
			switch in.OS {
			case "", "darwin", "linux", "windows":
			default:
				t.Errorf("%s: unknown OS %q", def.Name, in.OS)
			}
			if in.Auto && (in.Cmd[0] == "sudo" || in.Cmd[0] == "winget") {
				t.Errorf("%s: %q needs an administrator and must not be Auto", def.Name, strings.Join(in.Cmd, " "))
			}
		}
	}
}

func TestInstallRefusals(t *testing.T) {
	off := newLSPManager(t.TempDir(), false)
	if _, err := off.Install("gopls", 0); err == nil {
		t.Error("Install with language servers disabled succeeded")
	}
	m := &lspManager{enabled: true}
	if _, err := m.Install("px0-no-such-server", 0); err == nil {
		t.Error("Install of an unknown server succeeded")
	}
	if _, err := m.Install("gopls", 99); err == nil {
		t.Error("Install with an out-of-range option succeeded")
	}
	for _, def := range lspRegistry {
		for i, in := range def.installsFor(runtime.GOOS) {
			if !in.Auto {
				if _, err := m.Install(def.Name, i); err == nil {
					t.Errorf("Install ran the manual recipe %q", strings.Join(in.Cmd, " "))
				}
			}
		}
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	b := &tailBuffer{max: 8}
	b.Write([]byte("0123456789"))
	b.Write([]byte("ab"))
	if got := b.String(); got != "456789ab" {
		t.Errorf("tail = %q, want %q", got, "456789ab")
	}
}

func TestLSPServersEndpoint(t *testing.T) {
	s, root := newTestServer(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // the user's lsp settings stay out of it
	s.lsp = newLSPManager(root, true)
	t.Cleanup(s.lsp.Close)

	// Ensure we have Go and Python files
	_ = os.WriteFile(filepath.Join(root, "app.py"), []byte("print('hello')\n"), 0o644)
	s.ix.Build()

	req := httptest.NewRequest(http.MethodGet, "/api/lsp/servers", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var res struct {
		Enabled    bool             `json:"enabled"`
		AnyRunning bool             `json:"anyRunning"`
		Servers    []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json parse error: %v", err)
	}

	foundGo := false
	foundPy := false
	for _, srv := range res.Servers {
		name, _ := srv["name"].(string)
		if name == "gopls" {
			foundGo = true
		}
		if name == "pyright" || name == "pylsp" || name == "ruff" {
			foundPy = true
		}
	}
	if !foundGo {
		t.Errorf("expected gopls in relevant servers for Go files")
	}
	if !foundPy {
		t.Errorf("expected python language server in relevant servers for Python files")
	}
}

// The menu offers one server per language: the one that runs, else the best
// one px0 can install. An older tsc does not count as TypeScript's server.
func TestLSPPickOnePerLanguage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(invalidateBinaryCache) // forget the fake binaries for later tests
	put := func(name, out string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		invalidateBinaryCache()
	}
	group := func(lang string) []*lspServerDef {
		var g []*lspServerDef
		for i := range lspRegistry {
			if lspRegistry[i].Lang == lang {
				g = append(g, &lspRegistry[i])
			}
		}
		return g
	}
	pick := func(lang string) string { return lspPick(group(lang)).Name }

	invalidateBinaryCache()
	if got := pick("Python"); got != "pyright" {
		t.Errorf("nothing installed: Python offers %s, want pyright", got)
	}
	if got := pick("TypeScript and JavaScript"); got != "tsc" {
		t.Errorf("nothing installed: TypeScript offers %s, want tsc", got)
	}
	put("pylsp", "")
	if got := pick("Python"); got != "pylsp" {
		t.Errorf("pylsp installed: Python offers %s, want pylsp", got)
	}
	put("tsc", "Version 5.9.3")
	put("typescript-language-server", "")
	if got := pick("TypeScript and JavaScript"); got != "typescript" {
		t.Errorf("TypeScript 5: offers %s, want typescript", got)
	}
	put("tsc", "Version 7.0.2 ")
	if got := pick("TypeScript and JavaScript"); got != "tsc" {
		t.Errorf("TypeScript 7: offers %s, want tsc", got)
	}
}

func TestLSPStopAndStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LSP stop/start in short mode")
	}
	s, root := newTestServer(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s.lsp = newLSPManager(root, true)
	t.Cleanup(s.lsp.Close)
	_ = os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)

	// Stop gopls
	stopReq := httptest.NewRequest(http.MethodPost, "/api/lsp/stop?server=gopls", nil)
	stopReq.Host = "127.0.0.1:8080"
	stopReq.Header.Set("Origin", "http://127.0.0.1:8080")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, stopReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("stop returned code %d", rec.Code)
	}

	state, _ := s.lsp.State("main.go")
	if state != lspOff {
		t.Errorf("expected lspOff after stop, got %v", state)
	}

	// Verify Stop All
	stopAllReq := httptest.NewRequest(http.MethodPost, "/api/lsp/stop", nil)
	stopAllReq.Host = "127.0.0.1:8080"
	stopAllReq.Header.Set("Origin", "http://127.0.0.1:8080")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, stopAllReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("stop all returned code %d", rec.Code)
	}

	if s.lsp.AnyRunning() {
		t.Errorf("expected no servers running after stop all")
	}
}

// A server lsp.disabled lists is never chosen: TypeScript files get the next
// installed server from discovery and in the menu, a running one is stopped,
// and starting it by name is refused.
func TestLSPDisabledServerFallsToNext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(invalidateBinaryCache) // forget the fake binaries for later tests
	for name, out := range map[string]string{"tsc": "Version 7.0.2", "typescript-language-server": ""} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s, root := newTestServer(t)
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.ix.Build()
	m := &lspManager{
		root: root, enabled: true,
		clients:  map[string]*lspClient{},
		starting: map[string]chan struct{}{},
		failed:   map[string]string{},
		stopped:  map[string]bool{},
	}
	t.Cleanup(m.Close)
	s.lsp = m
	m.discover()
	if def := m.defFor("app.ts"); def == nil || def.Name != "tsc" {
		t.Fatalf("precondition: TypeScript 7 installed, .ts gets %v, want tsc", def)
	}
	var tsc lspServerDef
	for _, def := range lspRegistry {
		if def.Name == "tsc" {
			tsc = def
		}
	}
	running := newLSPClient(tsc, root)
	running.fail(errors.New("not a real process"))
	m.clients["tsc"] = running

	m.applySettings(time.Now(), settings{LSPDisabled: []string{"tsc"}})
	if def := m.defFor("app.ts"); def == nil || def.Name != "typescript" {
		t.Errorf("tsc disabled: .ts gets %v, want typescript", def)
	}
	if _, ok := m.clients["tsc"]; ok {
		t.Error("a running tsc was left running after being disabled")
	}
	offered := ""
	for _, srv := range s.relevantLSPServers() {
		if srv["fullLang"] == "TypeScript and JavaScript" {
			offered += srv["name"].(string) + " "
		}
	}
	if offered != "typescript " {
		t.Errorf("tsc disabled: the menu offers %q for TypeScript, want typescript only", offered)
	}
	if err := m.StartServer("tsc"); err == nil || err.Error() != "tsc is disabled in settings" {
		t.Errorf("StartServer(tsc) = %v, want tsc is disabled in settings", err)
	}
}

// A language is only reported as lacking a server when one could be installed
// and used: not while lsp.enabled pauses the servers, and not when lsp.disabled
// is why none was picked.
func TestLSPMissingLangSkipsDisabledAndPaused(t *testing.T) {
	def, _ := answeringServer(t)
	m := testLSPManager(t, def)
	m.discovered = true // and found no Go server
	s, _ := newTestServer(t)
	s.lsp = m
	if got := m.MissingLang("a.go"); got != "Go" {
		t.Fatalf("precondition: MissingLang with no Go server = %q, want Go", got)
	}
	if b := s.lspBrief("a.go"); b["missing"] != "Go" || b["paused"] != nil {
		t.Errorf("lspBrief with no Go server = %v, want missing Go and not paused", b)
	}

	m.mu.Lock()
	m.disabled = map[string]bool{"gopls": true}
	m.mu.Unlock()
	if got := m.MissingLang("a.go"); got != "" {
		t.Errorf("MissingLang with gopls disabled = %q, want none", got)
	}

	m.mu.Lock()
	m.disabled, m.paused = nil, true
	m.mu.Unlock()
	if got := m.MissingLang("a.go"); got != "" {
		t.Errorf("MissingLang with lsp.enabled off = %q, want none", got)
	}
	b := s.lspBrief("a.go")
	if b["paused"] != true || b["missing"] != nil {
		t.Errorf("lspBrief with lsp.enabled off = %v, want paused and nothing missing", b)
	}
	raw, _ := json.Marshal(s.relevantLSPServers())
	if string(raw) != "[]" {
		t.Errorf("relevantLSPServers with lsp.enabled off = %s, want []", raw)
	}

	// Only a stop by the user is reported as one.
	if m.ServerStatus(def, nil)["userStopped"] != false {
		t.Error("a never-started server is reported as stopped by the user")
	}
	m.stopped[def.Name] = true
	if m.ServerStatus(def, nil)["userStopped"] != true {
		t.Error("a server stopped by the user is not reported as such")
	}
}
