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

// testLSPManager serves def for its extensions, without discovery.
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
