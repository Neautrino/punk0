package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func resetNotes() {
	notes.Lock()
	notes.failed = map[string]failedFetch{}
	notes.Unlock()
}

func TestReleaseNotesFetchesThenServesTheCopy(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	resetNotes()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"tag_name":"v0.2.0","body":"### Added\n- **X.** y"}`))
	}))
	t.Setenv("PUNK0_UPDATE_URL", srv.URL)

	got, err := ReleaseNotes("0.2.0")
	if err != nil || got != "### Added\n- **X.** y" {
		t.Fatalf("ReleaseNotes = %q, %v", got, err)
	}
	if _, err := os.Stat(notesPath("0.2.0")); err != nil {
		t.Fatalf("notes not saved: %v", err)
	}
	srv.Close()
	if got, err := ReleaseNotes("0.2.0"); err != nil || got == "" {
		t.Fatalf("saved notes not used offline: %q, %v", got, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times, want 1", hits.Load())
	}
}

func TestReleaseNotesFailureIsNotRetriedAtOnce(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	resetNotes()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("PUNK0_UPDATE_URL", srv.URL)

	if _, err := ReleaseNotes("0.2.0"); err == nil {
		t.Fatal("want an error from a failing server")
	}
	if _, err := ReleaseNotes("0.2.0"); err == nil {
		t.Fatal("want the remembered error")
	}
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times, want 1", hits.Load())
	}
}

func TestReleaseNotesSavedAtInstallNeedNoNetwork(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PUNK0_UPDATE_URL", "http://127.0.0.1:1/should-not-be-hit")
	resetNotes()
	saveReleaseNotes("0.3.0", "### Fixed\n- **Z.**")
	if got, err := ReleaseNotes("0.3.0"); err != nil || got != "### Fixed\n- **Z.**" {
		t.Fatalf("ReleaseNotes = %q, %v", got, err)
	}
}
