package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"px0/internal/render"
)

// replaceRequest swaps lines l1..l2 for replacement, but only while they
// still read original: Quick Edit's preview was made from them, and an agent
// or the user may have changed the file since.
type replaceRequest struct {
	Path        string `json:"path"`
	L1          int    `json:"l1"`
	L2          int    `json:"l2"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"` // "" deletes the lines
}

func (s *Server) handleFileReplace(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req replaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	abs, rel, ok := s.safePath(req.Path)
	if !ok || rel == "" {
		fail(w, http.StatusBadRequest, "bad path")
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		fail(w, http.StatusNotFound, "file not found")
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	last := len(lines) // a trailing newline leaves an empty element that is not a line
	if last > 0 && lines[last-1] == "" {
		last--
	}
	if req.L1 < 1 || req.L2 < req.L1 || req.L2 > last {
		fail(w, http.StatusBadRequest, "line range out of bounds")
		return
	}

	// Compared without CR: the page reads lines without their endings.
	cur := make([]string, 0, req.L2-req.L1+1)
	for _, l := range lines[req.L1-1 : req.L2] {
		cur = append(cur, strings.TrimSuffix(l, "\r"))
	}
	if strings.Join(cur, "\n") != strings.ReplaceAll(req.Original, "\r\n", "\n") {
		fail(w, http.StatusConflict, "File was modified on disk; please review current lines")
		return
	}

	// The new lines take the file's own line endings, and the range's last line
	// keeps its own: a final line without a newline has no CR either.
	var rep []string
	if req.Replacement != "" {
		rep = strings.Split(strings.ReplaceAll(req.Replacement, "\r\n", "\n"), "\n")
		if strings.Contains(content, "\r\n") {
			for i := range rep {
				rep[i] += "\r"
			}
			if !strings.HasSuffix(lines[req.L2-1], "\r") {
				rep[len(rep)-1] = strings.TrimSuffix(rep[len(rep)-1], "\r")
			}
		}
	}
	out := make([]string, 0, len(lines)-(req.L2-req.L1+1)+len(rep))
	out = append(out, lines[:req.L1-1]...)
	out = append(out, rep...)
	out = append(out, lines[req.L2:]...)

	if err := os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		fail(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
		return
	}

	render.Evict(abs)
	if s.lsp != nil {
		s.lsp.CloseDoc(abs, rel)
		s.lsp.RefreshOpenDocs()
	}
	s.ix.TouchPath(rel)
	if s.gitWatcher != nil {
		s.gitWatcher.Trigger()
	}
	writeJSON(w, map[string]any{"ok": true})
}
