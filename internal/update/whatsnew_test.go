package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWhatsNewPending(t *testing.T) {
	cases := []struct {
		name     string
		file     string // whats_new.json before, "" for none
		firstRun bool
		want     string
		seen     string // seen in whats_new.json after
	}{
		{name: "first install records and shows nothing", firstRun: true, want: "", seen: "0.1.2"},
		{name: "existing install from before the file", want: "0.1.2"},
		{name: "older seen", file: `{"seen":"0.1.1"}`, want: "0.1.2", seen: "0.1.1"},
		{name: "same seen", file: `{"seen":"0.1.2"}`, want: "", seen: "0.1.2"},
		{name: "downgrade", file: `{"seen":"0.2.0"}`, want: "", seen: "0.2.0"},
		{name: "garbage on a first install", file: `{nope`, firstRun: true, want: "", seen: "0.1.2"},
		{name: "garbage on an existing install", file: `{nope`, want: "0.1.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if c.file != "" {
				if err := os.MkdirAll(filepath.Dir(whatsNewPath()), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(whatsNewPath(), []byte(c.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := WhatsNewPending("0.1.2", c.firstRun); got != c.want {
				t.Fatalf("WhatsNewPending = %q, want %q", got, c.want)
			}
			var s whatsNewState
			if b, err := os.ReadFile(whatsNewPath()); err == nil {
				_ = json.Unmarshal(b, &s)
			}
			if s.Seen != c.seen {
				t.Fatalf("seen afterwards = %q, want %q", s.Seen, c.seen)
			}
		})
	}
}

func TestMarkWhatsNewSeenClearsPending(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := WhatsNewPending("0.1.2", false); got != "0.1.2" {
		t.Fatalf("pending = %q, want 0.1.2", got)
	}
	MarkWhatsNewSeen("0.1.2")
	if got := WhatsNewPending("0.1.2", false); got != "" {
		t.Fatalf("pending after seen = %q, want none", got)
	}
	if got := WhatsNewPending("0.1.3", false); got != "0.1.3" {
		t.Fatalf("pending for the next version = %q, want 0.1.3", got)
	}
}
