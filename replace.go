package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"px0/internal/render"
)

type replaceRequest struct {
	Path        string `json:"path"`
	L1          int    `json:"l1"`
	L2          int    `json:"l2"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
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

	cleanPath, rel, ok := s.safePath(req.Path)
	if !ok || cleanPath == s.ix.Root() {
		fail(w, http.StatusBadRequest, "invalid file path")
		return
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		fail(w, http.StatusNotFound, "file not found")
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	if req.L1 < 1 || req.L2 < req.L1 || req.L1 > len(lines) {
		fail(w, http.StatusBadRequest, "line range out of bounds")
		return
	}

	endIdx := req.L2
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	targetSlice := lines[req.L1-1 : endIdx]
	currentBlock := strings.Join(targetSlice, "\n")

	// Optimistic concurrency check
	if strings.TrimRight(currentBlock, "\r") != strings.TrimRight(req.Original, "\r") {
		fail(w, http.StatusConflict, "File was modified on disk; please review current lines")
		return
	}

	repLines := strings.Split(req.Replacement, "\n")
	newLines := make([]string, 0, len(lines)-(endIdx-req.L1+1)+len(repLines))
	newLines = append(newLines, lines[:req.L1-1]...)
	newLines = append(newLines, repLines...)
	if endIdx < len(lines) {
		newLines = append(newLines, lines[endIdx:]...)
	}

	outContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(cleanPath, []byte(outContent), 0o644); err != nil {
		fail(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	render.Evict(cleanPath)
	if s.lsp != nil {
		s.lsp.CloseDoc(cleanPath, rel)
		s.lsp.RefreshOpenDocs()
	}
	s.ix.TouchPath(rel)
	if s.gitWatcher != nil {
		s.gitWatcher.Trigger()
	}

	writeJSON(w, map[string]any{"ok": true})
}
