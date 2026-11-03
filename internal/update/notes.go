package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"px0/internal/paths"
)

// notesRetry is how long a failed fetch of a release's notes is not tried
// again: unauthenticated GitHub allows 60 requests an hour, and every open
// What's New tab would ask.
const notesRetry = 10 * time.Minute

var notes = struct {
	sync.Mutex
	failed map[string]failedFetch   // version -> its last failed fetch
	flight map[string]chan struct{} // version -> closed when its fetch ends
}{failed: map[string]failedFetch{}, flight: map[string]chan struct{}{}}

type failedFetch struct {
	at  time.Time
	err error
}

func notesPath(ver string) string {
	return filepath.Join(paths.State(), "release-notes", "v"+ver+".md")
}

// saveReleaseNotes keeps a release's notes, so the What's New tab shows them
// offline, even before the restart into the version that brought them.
func saveReleaseNotes(ver, body string) {
	if body == "" {
		return
	}
	p := notesPath(ver)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	_ = os.WriteFile(p, []byte(body), 0o644)
}

// ReleaseNotes returns the markdown notes of release v<ver>: the copy saved
// when it was installed, else the body of its GitHub release, saved for next
// time. Callers asking for the same version at once share one fetch.
func ReleaseNotes(ver string) (string, error) {
	for {
		if b, err := os.ReadFile(notesPath(ver)); err == nil {
			return string(b), nil
		}
		notes.Lock()
		if f, ok := notes.failed[ver]; ok && time.Since(f.at) < notesRetry {
			notes.Unlock()
			return "", f.err
		}
		if wait, ok := notes.flight[ver]; ok {
			notes.Unlock()
			<-wait
			continue // the file, or the failure, is there now
		}
		done := make(chan struct{})
		notes.flight[ver] = done
		notes.Unlock()

		body, err := fetchNotes(ver)
		notes.Lock()
		delete(notes.flight, ver)
		if err != nil {
			notes.failed[ver] = failedFetch{at: time.Now(), err: err}
		} else {
			delete(notes.failed, ver)
		}
		notes.Unlock()
		close(done)
		return body, err
	}
}

func fetchNotes(ver string) (string, error) {
	rel, err := fetchRelease(getRepoName(), "v"+ver)
	if err != nil {
		return "", err
	}
	if rel.Body == "" {
		return "", errors.New("the release has no notes")
	}
	saveReleaseNotes(ver, rel.Body)
	return rel.Body, nil
}

// ReleasePageURL is the GitHub page of release v<ver>.
func ReleasePageURL(ver string) string {
	return fmt.Sprintf("https://github.com/%s/releases/tag/v%s", getRepoName(), ver)
}

// ChangelogURL is release v<ver> on the site's changelog.
func ChangelogURL(ver string) string {
	return "https://punk0.neautrino.dev/changelog/#v" + ver
}
