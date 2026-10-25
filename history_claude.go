package main

// Claude Code's history: one JSONL transcript per session under
// <config>/projects/<encoded cwd>/<sessionId>.jsonl. The folder names are
// encoded working directories, which punk0 never decodes: the records inside
// carry the cwd the session ran in, and that is what Chats mode groups
// by. Transcripts under a subagents/ folder are a conversation's own helpers,
// not chats of their own.
//
// A transcript is read in one pass: the cwd and the first prompt are at its
// head, the titles are appended at its end, and counting the messages needs
// all of it anyway. Only lines that can hold one of those are parsed, so most
// of a transcript is just scanned for a byte pattern. Claude Code has no
// delete command, so deleteClaudeChat unlinks a conversation itself, under
// rules strict enough that only a scanned transcript can match.

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// claudeLineMax is the longest transcript line punk0 reads; a pasted file
// makes a long one, and a longer one still ends that file's scan with
// whatever was found before it.
const claudeLineMax = 8 << 20

var (
	claudeUserType   = []byte(`"type":"user"`)
	claudeAsstType   = []byte(`"type":"assistant"`)
	claudeCustomType = []byte(`"type":"custom-title"`)
	claudeAIType     = []byte(`"type":"ai-title"`)
	claudeToolResult = []byte(`"toolUseResult"`)
)

// claudeRoot is where Claude Code keeps its state: CLAUDE_CONFIG_DIR, else
// ~/.claude. Read on each scan, so pointing a terminal at another config
// shows up without a restart.
func claudeRoot() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeRecord is the part of a transcript line punk0 reads.
type claudeRecord struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	GitBranch   string `json:"gitBranch"` // only the preview reads it
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// scanClaude adds every transcript Claude Code holds; seen collects the paths
// it found, so Sessions can drop the files that are gone.
func (h *chatHistory) scanClaude(ctx context.Context, out *[]chatSession, seen map[string]bool) {
	root := claudeRoot()
	if root == "" {
		return
	}
	filepath.WalkDir(filepath.Join(root, "projects"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder is no chats, not a failed list
		}
		if ctx.Err() != nil {
			return ctx.Err() // the page that asked is gone
		}
		if d.IsDir() {
			if d.Name() == "subagents" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[path] = true
		if c, ok := h.cached(path, info, readClaudeChat); ok {
			*out = append(*out, c)
		}
		return nil
	})
}

// readClaudeChat reads one transcript. The id is the file's name, which is
// the session id `claude --resume` takes. Messages counts what was said: the
// user's own prompts and the model's replies, not the meta records a slash
// command or a hook leaves behind.
func readClaudeChat(path string) (chatSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return chatSession{}, false
	}
	defer f.Close()
	c := chatSession{Agent: tabClaude, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), File: path}
	var custom, ai, prompt string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), claudeLineMax)
	for sc.Scan() {
		line := sc.Bytes()
		candidate := bytes.Contains(line, claudeUserType) || bytes.Contains(line, claudeAsstType) ||
			bytes.Contains(line, claudeCustomType) || bytes.Contains(line, claudeAIType)
		// Until the cwd is known every line is worth parsing: the first
		// records of a transcript are queue and prompt bookkeeping that
		// carry none.
		if !candidate && c.Dir != "" {
			continue
		}
		var r claudeRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if c.Dir == "" && r.Cwd != "" {
			c.Dir = r.Cwd
		}
		switch r.Type {
		case "user":
			// A tool result is a user record the user did not write
			// (toolUseResult beside the message), as are the meta records a
			// hook or a slash command leaves behind.
			if r.IsMeta || bytes.Contains(line, claudeToolResult) {
				break
			}
			text := claudeText(r.Message.Content)
			if !userText(text) {
				break
			}
			c.Messages++
			if prompt == "" && !r.IsSidechain {
				prompt = chatTitle(text)
			}
		case "assistant":
			// Claude writes one record per content block, so a reply that
			// calls tools spans several; only the ones with text are a reply.
			if claudeText(r.Message.Content) != "" {
				c.Messages++
			}
		case "custom-title":
			custom = r.CustomTitle
		case "ai-title":
			ai = r.AITitle
		}
	}
	if c.Dir == "" {
		return chatSession{}, false // nothing in it says where it ran
	}
	c.Title = cmp.Or(custom, ai, prompt, "(untitled)")
	return c, true
}

// claudeText is a message's text: Claude writes a string for a typed prompt
// and a list of blocks for anything richer.
func claudeText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// readClaudePreview reads the conversation into x by the same rules the
// title and the message count follow: no meta records, no tool results, no
// angle-bracket wrappers, and nothing from a sidechain, which is a subagent
// talking to itself rather than to the user. A reply is every assistant
// record after the prompt -- one per content block -- of which only the text
// ones were said to the user. The branch is the last one the records carry.
func readClaudePreview(path string, p *chatPreview, x *exchange) error {
	return previewLines(path, func(line []byte) {
		if !bytes.Contains(line, claudeUserType) && !bytes.Contains(line, claudeAsstType) {
			return
		}
		var r claudeRecord
		if json.Unmarshal(line, &r) != nil || r.IsSidechain {
			return
		}
		if r.GitBranch != "" {
			p.Branch = r.GitBranch
		}
		switch r.Type {
		case "user":
			if r.IsMeta || bytes.Contains(line, claudeToolResult) {
				return
			}
			x.user(claudeText(r.Message.Content))
		case "assistant":
			x.agent(claudeText(r.Message.Content))
		}
	})
}

// deleteClaudeChat takes a conversation out of Claude Code's history, which
// has no delete command of its own: the transcript and the <id>/ folder of
// subagent transcripts beside it, and nothing else. punk0 is unlinking files
// for the user here, so every part of the path is checked against what a scan
// can legitimately have found, and a mismatch is an error, never a silent
// skip. Lstat, not Stat: a symlink named <id> is not this chat's folder.
func deleteClaudeChat(c chatSession) error {
	if c.File == "" {
		return errors.New("this chat has no transcript to delete")
	}
	root := claudeRoot()
	dir := filepath.Dir(c.File)
	// The folder itself is resolved too, not just its parent: <folder> being
	// a symlink would otherwise unlink inside whatever it points at.
	if root == "" || filepath.Dir(resolvePath(dir)) != resolvePath(filepath.Join(root, "projects")) {
		return fmt.Errorf("%s is not one of Claude Code's transcript folders", tildePath(dir))
	}
	if filepath.Base(c.File) != c.ID+".jsonl" {
		return fmt.Errorf("%s is not the transcript of chat %s", tildePath(c.File), c.ID)
	}
	if err := os.Remove(filepath.Join(dir, c.ID+".jsonl")); err != nil {
		return err
	}
	side := filepath.Join(dir, c.ID)
	if fi, err := os.Lstat(side); err == nil && fi.IsDir() {
		return os.RemoveAll(side)
	}
	return nil
}
