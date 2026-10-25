package main

// Codex's history: one rollout JSONL per session under
// <home>/sessions/YYYY/MM/DD/rollout-<stamp>-<id>.jsonl, whose session_meta
// head record holds the id `codex resume` takes and the folder it ran in.
// Titles live apart, in <home>/session_index.jsonl, one line per rename with
// the last line for an id winning, so a renamed thread shows its new name
// without its rollout changing. Compressed rollouts (.jsonl.zst) are left
// out: punk0 carries no zstd decoder.

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var codexItemType = []byte(`"type":"response_item"`)

// codexRoot is CODEX_HOME, else ~/.codex, read on each scan like claudeRoot.
func codexRoot() string {
	if d := strings.TrimSpace(os.Getenv("CODEX_HOME")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// codexRecord is the part of a rollout line punk0 reads: the session_meta
// head, and the messages it counts and names the chat from.
type codexRecord struct {
	Type    string `json:"type"`
	Payload struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
		Type      string `json:"type"`
		Role      string `json:"role"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

// codexIndex is session_index.jsonl, cached by the file's mtime and size like
// a transcript.
type codexIndex struct {
	mod     time.Time
	size    int64
	entries map[string]codexEntry
}

type codexEntry struct {
	title   string
	updated int64 // unix ms
}

// scanCodex adds every rollout Codex holds, with the index's name and time
// laid over what the rollout itself says.
func (h *chatHistory) scanCodex(ctx context.Context, out *[]chatSession, seen map[string]bool) {
	root := codexRoot()
	if root == "" {
		return
	}
	h.loadCodexIndex(filepath.Join(root, "session_index.jsonl"))
	filepath.WalkDir(filepath.Join(root, "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if name := d.Name(); !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[path] = true
		c, ok := h.cached(path, info, readCodexChat)
		if !ok {
			return nil
		}
		if e, ok := h.index.entries[c.ID]; ok {
			c.Title = cmp.Or(e.title, c.Title)
			// The index is written on rename, so a thread renamed early and
			// talked to for hours is newer than its line there.
			c.Updated = max(c.Updated, e.updated)
		}
		*out = append(*out, c)
		return nil
	})
}

// loadCodexIndex re-reads session_index.jsonl when it changed. A file that
// cannot be opened is remembered as empty, so a broken index is not reread on
// every scan. Called with h.mu held.
func (h *chatHistory) loadCodexIndex(path string) {
	info, err := os.Stat(path)
	if err != nil {
		h.index = codexIndex{}
		return
	}
	if h.index.entries != nil && h.index.mod.Equal(info.ModTime()) && h.index.size == info.Size() {
		return
	}
	h.index = codexIndex{mod: info.ModTime(), size: info.Size(), entries: map[string]codexEntry{}}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var e struct {
			ID      string `json:"id"`
			Name    string `json:"thread_name"`
			Updated string `json:"updated_at"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.ID == "" {
			continue
		}
		entry := codexEntry{title: e.Name}
		if t, err := time.Parse(time.RFC3339Nano, e.Updated); err == nil {
			entry.updated = t.UnixMilli()
		}
		h.index.entries[e.ID] = entry
	}
}

// readCodexChat reads one rollout: its head names the session, and the rest
// is scanned for the messages it counts -- the user's prompts and the
// model's replies, not the developer instructions Codex injects.
func readCodexChat(path string) (chatSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return chatSession{}, false
	}
	defer f.Close()
	c := chatSession{Agent: "codex", File: path}
	prompt := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), claudeLineMax)
	for sc.Scan() {
		line := sc.Bytes()
		if c.ID != "" && !bytes.Contains(line, codexItemType) {
			continue
		}
		var r codexRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		switch r.Type {
		case "session_meta":
			c.ID, c.Dir = cmp.Or(r.Payload.ID, r.Payload.SessionID), r.Payload.Cwd
		case "response_item":
			if r.Payload.Type != "message" {
				continue
			}
			text := ""
			if len(r.Payload.Content) > 0 {
				text = r.Payload.Content[0].Text
			}
			switch r.Payload.Role {
			case "user":
				if !userText(text) {
					continue
				}
				c.Messages++
				if prompt == "" {
					prompt = chatTitle(text)
				}
			case "assistant":
				c.Messages++
			}
		}
	}
	if c.ID == "" || c.Dir == "" {
		return chatSession{}, false
	}
	c.Title = cmp.Or(prompt, "(untitled)")
	return c, true
}

// readCodexPreview reads the rollout into x. Only response_item messages
// count, and only the user's and the model's: the developer instructions
// Codex injects and the <environment_context> block it wraps around the
// folder are nobody's prompt.
func readCodexPreview(path string, _ *chatPreview, x *exchange) error {
	return previewLines(path, func(line []byte) {
		if !bytes.Contains(line, codexItemType) {
			return
		}
		var r codexRecord
		if json.Unmarshal(line, &r) != nil || r.Type != "response_item" || r.Payload.Type != "message" {
			return
		}
		text := ""
		if len(r.Payload.Content) > 0 {
			text = r.Payload.Content[0].Text
		}
		switch r.Payload.Role {
		case "user":
			x.user(text)
		case "assistant":
			x.agent(text)
		}
	})
}
