package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"px0/internal/paths"
)

// legacyHome is a HOME holding an old ~/.punk0 install, with no XDG variables
// pointing anywhere else.
func legacyHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, "")
	}
	for _, f := range []string{"records.json", "settings.json", "logs/punk0.log", "worktrees/p/x/file", "cache/vendor/x", "claude-hooks.json"} {
		p := filepath.Join(home, ".punk0", filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestMigrateMovesStateNotCode(t *testing.T) {
	home := legacyHome(t)
	legacy := filepath.Join(home, ".punk0")

	var out bytes.Buffer
	if code := migrate(&out); code != 0 {
		t.Fatalf("migrate = %d: %s", code, out.String())
	}

	for _, p := range []string{
		filepath.Join(home, ".config", "punk0", "settings.json"),
		filepath.Join(home, ".local", "state", "punk0", "records.json"),
		filepath.Join(home, ".local", "state", "punk0", "logs", "punk0.log"),
		filepath.Join(legacy, "worktrees", "p", "x", "file"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing after migrate: %s", p)
		}
	}
	for _, p := range []string{
		filepath.Join(legacy, "records.json"),
		filepath.Join(legacy, "settings.json"),
		filepath.Join(legacy, "logs"),
		filepath.Join(legacy, "cache"),
		filepath.Join(legacy, "claude-hooks.json"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("still in the legacy folder: %s (%v)", p, err)
		}
	}
	if got, want := paths.State(), paths.StandardState(); got != want {
		t.Errorf("State = %s, want %s", got, want)
	}
	if got, want := paths.Config(), paths.StandardConfig(); got != want {
		t.Errorf("Config = %s, want %s", got, want)
	}

	// A second run has nothing left to do.
	out.Reset()
	if code := migrate(&out); code != 0 {
		t.Fatalf("second migrate = %d: %s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("already uses")) {
		t.Errorf("second migrate said %q", out.String())
	}
}

func TestMigrateRefusesWhileRunning(t *testing.T) {
	home := legacyHome(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wb/ping" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"workbench":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()
	writeWorkbenchLock(ts.URL)

	var out bytes.Buffer
	if code := migrate(&out); code != 1 {
		t.Fatalf("migrate = %d while running: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".punk0", "records.json")); err != nil {
		t.Errorf("records.json moved while punk0 was running: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "punk0", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("settings moved while punk0 was running: %v", err)
	}
}
