package main

// OpenCode's history: its sessions live in a database no one but its own CLI
// reads, so punk0 asks the CLI. `opencode session list` answers for the
// project of the folder it runs in, so the list is as wide as the folders
// punk0 knows (its projects and their workspaces) -- a chat of a folder punk0
// has never seen is not listed at all. The listing costs a process per folder,
// so it never runs on a request: a scan starts one round in the background at
// most every opencodeEvery and lists what the last round returned. Message
// counts are not in the answer, so they stay 0.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	opencodeEvery = 10 * time.Second
	opencodeMax   = "200" // sessions asked for per folder
	opencodeJobs  = 4     // folders asked at once
	// opencodePreview caps `session export`: a click waits for it, so it may
	// not take the whole chatCLITimeout.
	opencodePreview = 5 * time.Second
)

// opencodeSessions returns the rows of the last round and starts another when
// the last one is older than opencodeEvery. An `opencode` that is no longer
// on PATH answers nothing ever again, so its last rows go rather than stay
// listed until punk0 restarts. Called with h.mu held.
func (h *chatHistory) opencodeSessions() []chatSession {
	if !h.cli.look("opencode") {
		h.opencode = nil
		return nil
	}
	if !h.asking && time.Since(h.asked) >= opencodeEvery {
		h.asking = true
		go h.askOpenCode()
	}
	return slices.Clone(h.opencode)
}

// askOpenCode asks every folder punk0 knows, opencodeJobs at a time, with a
// context of its own: the page that started the round may be gone before it
// ends. Rows are deduped by id, since two folders of one project answer the
// same sessions. A round in which no folder answered keeps the last good
// rows, so a `opencode` upgrade passing through does not empty the list.
func (h *chatHistory) askOpenCode() {
	h.mu.Lock()
	gen := h.askGen
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), chatCLITimeout)
	defer cancel()
	var dirs []string
	if h.dirs != nil {
		dirs = h.dirs()
	}
	var (
		mu     sync.Mutex
		seen   = map[string]bool{}
		out    []chatSession
		answer bool
	)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range min(opencodeJobs, len(dirs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dir := range jobs {
				rows, ok := h.opencodeIn(ctx, dir)
				mu.Lock()
				answer = answer || ok
				for _, c := range rows {
					if !seen[c.ID] {
						seen[c.ID] = true
						out = append(out, c)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, dir := range dirs {
		jobs <- dir
	}
	close(jobs)
	wg.Wait()

	h.mu.Lock()
	defer h.mu.Unlock()
	h.asking = false
	// A Sync while this round ran asked for a fresh one: leaving h.asked zero
	// starts it on the next list rather than opencodeEvery from now.
	if gen == h.askGen {
		h.asked = time.Now()
	}
	if answer {
		h.opencode = out
	}
}

// opencodeIn lists one folder's sessions, and says whether the CLI answered
// at all: an empty answer is a folder with no chats, an error -- including
// output that is not the JSON list -- is a folder to leave out of this round.
func (h *chatHistory) opencodeIn(ctx context.Context, dir string) ([]chatSession, bool) {
	out, err := h.cli.run(ctx, dir, "opencode", "session", "list", "--format", "json", "-n", opencodeMax)
	if err != nil {
		return nil, false
	}
	var rows []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Updated int64  `json:"updated"` // unix ms
		Created int64  `json:"created"`
		Dir     string `json:"directory"`
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, true // a folder with no chats, not a broken round
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil, false
	}
	sessions := make([]chatSession, 0, len(rows))
	for _, r := range rows {
		// The id goes to `opencode session export` as an argument and into
		// Resume's tab command: one the CLI's own JSON made up, such as
		// `--help`, is not a chat punk0 will list.
		if !validSession.MatchString(r.ID) {
			continue
		}
		// No message count: the listing holds none, and a count per session
		// would be a process each.
		sessions = append(sessions, chatSession{
			Agent:   "opencode",
			ID:      r.ID,
			Dir:     cmp.Or(r.Dir, dir),
			Title:   cmp.Or(strings.TrimSpace(r.Title), "(untitled)"),
			Updated: cmp.Or(r.Updated, r.Created),
		})
	}
	return sessions, true
}

// readOpenCodePreview fills p from `opencode session export`, which writes
// the whole conversation as JSON: a user message carries its text directly,
// an assistant message a list of blocks of which only the text ones were
// shown (reasoning and tool blocks are not part of a reply).
func (h *chatHistory) readOpenCodePreview(c chatSession, p *chatPreview, x *exchange) error {
	ctx, cancel := context.WithTimeout(context.Background(), opencodePreview)
	defer cancel()
	out, err := h.cli.run(ctx, runDir(c.Dir), "opencode", "session", "export", c.ID)
	if err != nil {
		return err
	}
	var ex struct {
		Info struct {
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		} `json:"info"`
		Messages []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &ex); err != nil {
		return err
	}
	if d := ex.Info.Location.Directory; d != "" {
		p.Dir = d
	}
	for _, m := range ex.Messages {
		switch m.Type {
		case "user":
			x.user(m.Text)
		case "assistant":
			for _, b := range m.Content {
				if b.Type == "text" {
					x.agent(b.Text)
				}
			}
		}
	}
	return nil
}
