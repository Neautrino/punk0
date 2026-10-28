package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// home gives the test its own HOME with no XDG variables, the layout most
// users have.
func testHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(v, "")
	}
	return h
}

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStandardFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	h := testHome(t)
	for _, c := range []struct{ name, got, want string }{
		{"config", Config(), filepath.Join(h, ".config", "punk0")},
		{"state", State(), filepath.Join(h, ".local", "state", "punk0")},
		{"cache", Cache(), filepath.Join(h, ".cache", "punk0")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.name, c.got, c.want)
		}
	}
}

func TestXDGVariablesWin(t *testing.T) {
	h := testHome(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "st"))
	touch(t, filepath.Join(h, ".punk0", "records.json")) // a legacy install
	if got, want := State(), filepath.Join(h, "st", "punk0"); got != want {
		t.Errorf("State = %s, want %s: a set XDG variable never falls back", got, want)
	}
	t.Setenv("XDG_STATE_HOME", "relative/st") // invalid per the XDG spec: ignored
	if got, want := State(), filepath.Join(h, ".punk0"); got != want {
		t.Errorf("State = %s, want the legacy %s", got, want)
	}
}

func TestLegacyFolderKeptUntilMigrated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	h := testHome(t)
	legacy := filepath.Join(h, ".punk0")
	touch(t, filepath.Join(legacy, "records.json"))
	touch(t, filepath.Join(legacy, "settings.json"))
	if State() != legacy || Config() != legacy {
		t.Fatalf("legacy install: State=%s Config=%s, want %s", State(), Config(), legacy)
	}
	if StandardState() != filepath.Join(h, ".local", "state", "punk0") {
		t.Errorf("StandardState = %s", StandardState())
	}
	// Once the standard folder has the file, it wins.
	touch(t, filepath.Join(StandardState(), "records.json"))
	if State() != StandardState() {
		t.Errorf("State = %s after migration", State())
	}
	if Config() != legacy {
		t.Errorf("Config moved with State: each folder decides by its own file")
	}
}
