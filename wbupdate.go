package main

// The agents page's side of updates (web/src/orch/update.js, whatsnew.js):
// the version auto-update installed while punk0 runs, the version whose
// release notes the page opens once after a version change, and Restart now,
// which runWorkbench carries out (main.go).

import (
	"log/slog"
	"net/http"
	"regexp"
	"sync"

	"px0/internal/render"
	"px0/internal/update"
)

type wbUpdate struct {
	mu       sync.Mutex
	update   string        // installed in place, running only after a restart
	whatsNew string        // notes the page opens once; "" once opened
	restart  chan struct{} // buffered 1: a Restart now waiting for runWorkbench
}

func (u *wbUpdate) setInstalled(v string) {
	u.mu.Lock()
	u.update = v
	u.mu.Unlock()
}

func (u *wbUpdate) installed() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.update
}

func (u *wbUpdate) setPendingNotes(v string) {
	u.mu.Lock()
	u.whatsNew = v
	u.mu.Unlock()
}

func (u *wbUpdate) pendingNotes() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.whatsNew
}

// releaseVersion is what ?v= may name: a release version, nothing that could
// turn into another path or URL.
var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)

// handleWhatsNew answers the release notes of ?v= (the running version when
// empty) as HTML with raw HTML dropped, and where the release and the
// changelog are; html is "" when the notes cannot be had, and the tab links
// to those pages instead.
func (wb *workbench) handleWhatsNew(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	v := r.URL.Query().Get("v")
	if v == "" {
		v = version
	}
	if !releaseVersion.MatchString(v) {
		fail(w, http.StatusBadRequest, "not a release version")
		return
	}
	html := ""
	if md, err := update.ReleaseNotes(v); err != nil {
		slog.Warn("release notes unavailable", "version", v, "err", err)
	} else if html, err = render.RenderSafeMarkdown([]byte(md)); err != nil {
		slog.Warn("release notes did not render", "version", v, "err", err)
	}
	writeJSON(w, map[string]any{
		"version":   v,
		"html":      html,
		"release":   update.ReleasePageURL(v),
		"changelog": update.ChangelogURL(v),
	})
}

// handleWhatsNewSeen is the page saying it opened the notes: other pages
// then do not open them too, and the next start does not either.
func (wb *workbench) handleWhatsNewSeen(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	update.MarkWhatsNewSeen(version)
	wb.upd.setPendingNotes("")
	writeJSON(w, map[string]any{"ok": true})
}

// handleRestart asks runWorkbench to stop every tab and child and run the
// installed binary in place. The reply goes out first; a request while one
// is already waiting changes nothing.
func (wb *workbench) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	select {
	case wb.upd.restart <- struct{}{}:
	default:
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
}
