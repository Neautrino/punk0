package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"px0/internal/ui"
)

// pageHub holds the agents pages open in browsers, each listening on
// GET /api/wb/events, so a second `punk0 <dir>` can switch one to its folder
// instead of opening another browser tab.
type pageHub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
	quit chan struct{} // closed by Close: the streams end so Shutdown is not held up
	once sync.Once
}

func newPageHub() *pageHub {
	return &pageHub{subs: map[chan string]struct{}{}, quit: make(chan struct{})}
}

func (h *pageHub) subscribe() chan string {
	ch := make(chan string, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *pageHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// show asks every open page to select workspace id and reports how many
// pages there are. A page that has not taken the last request yet gets this
// one instead: only the newest selection matters.
func (h *pageHub) show(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case <-ch:
		default:
		}
		ch <- id
	}
	return len(h.subs)
}

func (h *pageHub) Close() { h.once.Do(func() { close(h.quit) }) }

// pageHeartbeat keeps an idle stream from being cut by anything in between.
const pageHeartbeat = 30 * time.Second

// handlePageEvents streams `select` events, each a workspace id, to one
// agents page until it goes away or punk0 stops.
func (wb *workbench) handlePageEvents(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ch := wb.pages.subscribe()
	defer wb.pages.unsubscribe(ch)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	beat := time.NewTicker(pageHeartbeat)
	defer beat.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-wb.pages.quit:
			return
		case id := <-ch:
			_, err = fmt.Fprintf(w, "event: select\ndata: %s\n\n", id)
		case <-beat.C:
			_, err = fmt.Fprint(w, ": \n\n")
		}
		if err != nil {
			return
		}
		flusher.Flush()
	}
}

// handlePageShow takes {"workspace": id} from a second punk0 and has every
// open agents page select it, answering {"pages": n}; with none open, the
// caller opens a browser tab instead.
func (wb *workbench) handlePageShow(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Workspace string `json:"workspace"`
	}
	if readJSONBody(r, &body) != nil || body.Workspace == "" {
		fail(w, http.StatusBadRequest, "workspace is required")
		return
	}
	// Known ids only: the id goes into an event line as it is.
	if wb.records.Workspace(body.Workspace) == nil {
		fail(w, http.StatusNotFound, "no such workspace")
		return
	}
	n := wb.pages.show(body.Workspace)
	note := "from another terminal"
	if n == 0 {
		note += ", no agents page open"
	}
	ui.Event("add", "opened", wb.records.WorkspaceLabel(body.Workspace), note, os.Stdout)
	writeJSON(w, map[string]any{"pages": n})
}
