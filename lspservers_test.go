package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A server that refuses initialize but keeps running is shut down, not left
// holding memory with nothing ever talking to it again.
func TestRefusedServerDoesNotLinger(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	body := `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"no TypeScript"}}`
	script := `echo $$ > "$1"; printf 'Content-Length: %d\r\n\r\n%s' "${#2}" "$2"; exec sleep 30`
	def := &lspServerDef{Name: "refuses", Cmd: []string{"sh", "-c", script, "sh", pidFile, body}, Exts: []string{".x"}}
	m := testLSPManager(t, def)
	done := make(chan struct{})
	m.starting[def.Name] = done
	m.spawn(def, done)
	if _, bad := m.failed[def.Name]; !bad {
		t.Fatal("refused initialize was not recorded as a failure")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	proc, _ := os.FindProcess(pid)
	for deadline := time.Now().Add(5 * time.Second); proc.Signal(syscall.Signal(0)) == nil; {
		if time.Now().After(deadline) {
			t.Fatalf("server %d still running after its initialize failed", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Only TypeScript 7's tsc serves LSP; an older tsc on PATH must not claim
// .ts files, or every TypeScript file would get a server that cannot start.
func TestTSNativeNeedsTypeScript7(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	for _, c := range []struct {
		out  string
		want bool
	}{
		{"Version 7.0.2", true},
		{"Version 7.1.0-dev.20261022.1", true},
		{"Version 10.0.0", true},
		{"Version 6.0.3", false},
		{"Version 5.9.3", false},
		{"", false},
	} {
		bin := filepath.Join(dir, "tsc")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '"+c.out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := tsNative(bin); got != c.want {
			t.Errorf("tsc printing %q: native = %v, want %v", c.out, got, c.want)
		}
	}
}

// A crashed client is replaced by a fresh spawn, a bounded number of times.
func TestCrashedServerIsRespawned(t *testing.T) {
	def := lspServerDef{Name: "gone", Cmd: []string{"px0-test-no-such-language-server"}, Exts: []string{".go"}}
	dead := func() *lspClient {
		c := newLSPClient(def, t.TempDir())
		c.fail(errors.New("gone exited"))
		return c
	}
	m := &lspManager{
		root: t.TempDir(), enabled: true,
		byExt:    map[string]*lspServerDef{".go": &def},
		clients:  map[string]*lspClient{"gone": dead()},
		starting: map[string]chan struct{}{},
		failed:   map[string]string{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The respawn is attempted; the binary does not exist, so it fails to start.
	if _, err := m.client(ctx, "x.go"); !isErrFailed(err) {
		t.Fatalf("client() = %v, want a failed respawn rather than the stale crash", err)
	}
	if m.restarts["gone"] != 1 {
		t.Errorf("restarts = %d, want 1", m.restarts["gone"])
	}

	// Out of budget: the crash is reported and nothing is spawned.
	m.mu.Lock()
	delete(m.failed, "gone")
	m.clients["gone"] = dead()
	m.restarts["gone"] = maxLSPRestarts
	m.mu.Unlock()
	if _, err := m.client(ctx, "x.go"); err == nil || isErrFailed(err) {
		t.Errorf("client() past the restart budget = %v, want the crash error", err)
	}
}

func isErrFailed(err error) bool {
	_, ok := err.(errFailed)
	return ok
}

// answeringServer is a language server that accepts initialize and then
// idles; each start appends its pid to the returned file.
func answeringServer(t *testing.T) (*lspServerDef, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	pidFile := filepath.Join(t.TempDir(), "pids")
	body := `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	script := `echo $$ >> "$1"; printf 'Content-Length: %d\r\n\r\n%s' "${#2}" "$2"; exec sleep 30`
	return &lspServerDef{Name: "answers", Cmd: []string{"sh", "-c", script, "sh", pidFile, body}, Exts: []string{".x"}}, pidFile
}

// testLSPManager serves def for its extensions, without discovery or the
// settings reaper, so a test drives applySettings itself.
func testLSPManager(t *testing.T, def *lspServerDef) *lspManager {
	m := &lspManager{
		root: t.TempDir(), enabled: true,
		byExt:    map[string]*lspServerDef{},
		clients:  map[string]*lspClient{},
		starting: map[string]chan struct{}{},
		failed:   map[string]string{},
		stopped:  map[string]bool{},
	}
	for _, ext := range def.Exts {
		m.byExt[ext] = def
	}
	t.Cleanup(m.Close)
	return m
}

// lastPid is the pid of the latest start recorded in pidFile.
func lastPid(t *testing.T, pidFile string) int {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(raw))
	pid, _ := strconv.Atoi(lines[len(lines)-1])
	return pid
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	proc, _ := os.FindProcess(pid)
	for deadline := time.Now().Add(5 * time.Second); proc.Signal(syscall.Signal(0)) == nil; {
		if time.Now().After(deadline) {
			t.Fatalf("server process %d still running", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A server nobody has used for lsp.idleStop is stopped while its folder stays
// open, a use resets that clock, and the next request after a stop starts it
// again instead of finding it stopped as if by the user.
func TestLSPIdleServerStopsAndRestartsOnUse(t *testing.T) {
	def, pids := answeringServer(t)
	m := testLSPManager(t, def)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := m.client(ctx, "a.x")
	if err != nil {
		t.Fatal(err)
	}
	running := func() *lspClient {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.clients[def.Name]
	}
	idle, never := 10.0, 0.0
	cfg := settings{LSPIdleStop: &idle}

	if m.applySettings(time.Now().Add(1000*time.Hour), settings{LSPIdleStop: &never}); running() != first {
		t.Fatal("lsp.idleStop 0 stopped a server")
	}
	// Unused since long before the check, but handed out just now.
	m.mu.Lock()
	m.used[def.Name].Store(time.Now().Add(-time.Hour).UnixNano())
	m.mu.Unlock()
	if c, err := m.client(ctx, "a.x"); err != nil || c != first {
		t.Fatalf("client() = %v, %v; want the running server", c, err)
	}
	if m.applySettings(time.Now().Add(9*time.Minute), cfg); running() != first {
		t.Fatal("stopped a server used 9 minutes ago with lsp.idleStop 10")
	}

	pid := lastPid(t, pids)
	m.applySettings(time.Now().Add(11*time.Minute), cfg)
	if running() != nil {
		t.Fatal("a server idle for 11 minutes kept running with lsp.idleStop 10")
	}
	waitGone(t, pid)
	again, err := m.client(ctx, "a.x")
	if err != nil {
		t.Fatalf("next use after the idle stop: %v", err)
	}
	if again == first || again.alive() != nil || lastPid(t, pids) == pid {
		t.Error("the next use did not start a fresh server")
	}
}

// lsp.enabled off stops what runs and refuses new starts, from requests and
// from the menu alike; turned back on, the next use starts a server.
func TestLSPEnabledOffStopsAndRefuses(t *testing.T) {
	def, pids := answeringServer(t)
	m := testLSPManager(t, def)
	s, _ := newTestServer(t)
	s.lsp = m
	if len(s.relevantLSPServers()) == 0 {
		t.Fatal("precondition: a folder with Go and Python files lists no servers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := m.client(ctx, "a.x")
	if err != nil {
		t.Fatal(err)
	}
	pid := lastPid(t, pids)

	off, on := false, true
	m.applySettings(time.Now(), settings{LSPEnabled: &off})
	waitGone(t, pid)
	if _, err := m.client(ctx, "a.x"); err != errPaused {
		t.Errorf("client() with lsp.enabled off = %v, want %v", err, errPaused)
	}
	if got := s.relevantLSPServers(); len(got) != 0 {
		t.Errorf("relevantLSPServers with lsp.enabled off = %v, want none", got)
	}
	if err := m.StartServer("gopls"); err == nil {
		t.Error("StartServer with lsp.enabled off succeeded")
	}
	if st, _ := m.State("a.x"); st != lspOff {
		t.Errorf("State with lsp.enabled off = %s, want off", st)
	}

	m.applySettings(time.Now(), settings{LSPEnabled: &on})
	again, err := m.client(ctx, "a.x")
	if err != nil {
		t.Fatalf("client() after lsp.enabled on again: %v", err)
	}
	if again == first || again.alive() != nil {
		t.Error("turning lsp.enabled back on did not start a fresh server")
	}
}

// A change to the settings file, made by any page or tool, reaches the folder's
// servers on the reaper's next look, which reads the file only when it changed.
func TestLSPSettingsFileChangeIsPickedUp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	def, _ := answeringServer(t)
	m := testLSPManager(t, def)
	now := time.Now()
	m.settingsSeen, m.swept = statSettings(), now
	if m.checkSettings(now.Add(time.Second)) {
		t.Fatal("applied an unchanged settings file before lspReapEvery")
	}

	if err := updateSettingsMap(map[string]any{"lsp.enabled": false}); err != nil {
		t.Fatal(err)
	}
	if !m.checkSettings(now.Add(time.Second)) || m.Enabled() {
		t.Fatal("a written lsp.enabled false was not applied on the next look")
	}
	if m.checkSettings(now.Add(2 * time.Second)) {
		t.Error("applied the settings again though the file had not changed")
	}
	if !m.checkSettings(now.Add(2*time.Second + lspReapEvery)) {
		t.Error("no sweep after lspReapEvery, so idle servers would never stop")
	}

	// The reaper itself: turned back on, the folder sees it within seconds.
	m.quit = make(chan struct{})
	go m.reap()
	if err := updateSettingsMap(map[string]any{"lsp.enabled": true}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2*lspSettingsEvery + time.Second); !m.Enabled(); {
		if time.Now().After(deadline) {
			t.Fatal("the reaper did not pick up lsp.enabled true from the file")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
