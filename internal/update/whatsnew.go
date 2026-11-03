package update

import (
	"encoding/json"
	"os"
	"path/filepath"

	"px0/internal/paths"
)

// whatsNewState is the last version whose notes the agents page opened.
type whatsNewState struct {
	Seen string `json:"seen"`
}

func whatsNewPath() string { return filepath.Join(paths.State(), "whats_new.json") }

// WhatsNewPending returns the version whose notes the agents page should open
// once, or "". firstRun says punk0 has never run here: a first install shows
// nothing and only records the version, while an install from before
// whats_new.json existed counts as an upgrade.
func WhatsNewPending(version string, firstRun bool) string {
	var s whatsNewState
	if b, err := os.ReadFile(whatsNewPath()); err == nil && json.Unmarshal(b, &s) == nil && s.Seen != "" {
		if compareSemver(version, s.Seen) > 0 {
			return version
		}
		return ""
	}
	if firstRun {
		MarkWhatsNewSeen(version)
		return ""
	}
	return version
}

// MarkWhatsNewSeen records that the notes of version were opened.
func MarkWhatsNewSeen(version string) {
	p := whatsNewPath()
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	if b, err := json.Marshal(whatsNewState{Seen: version}); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
