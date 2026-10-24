package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/px0-ai/harness"
)

type quickEditRequest struct {
	Path        string `json:"path"`
	L1          int    `json:"l1"`
	L2          int    `json:"l2"`
	Selection   string `json:"selection"`
	Instruction string `json:"instruction"`
}

var (
	reReplacement = regexp.MustCompile(`(?s)<replacement>(.*?)</replacement>`)
	reCodeBlock   = regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_-]+)?\n(.*?)\n```")
)

func parseQuickEditOutput(out string) (string, error) {
	tagMatches := reReplacement.FindAllStringSubmatch(out, -1)
	if len(tagMatches) > 0 {
		return strings.TrimSpace(tagMatches[len(tagMatches)-1][1]), nil
	}
	blockMatches := reCodeBlock.FindAllStringSubmatch(out, -1)
	if len(blockMatches) > 0 {
		return strings.TrimSpace(blockMatches[len(blockMatches)-1][1]), nil
	}
	return "", errors.New("no replacement tags or code block found in agent output")
}

func (s *Server) handleQuickEdit(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req quickEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.agent == nil {
		fail(w, http.StatusBadRequest, "agent manager not available")
		return
	}

	harnessName, baseArgv, model := s.agent.current()
	if harnessName == "" || len(baseArgv) == 0 {
		fail(w, http.StatusBadRequest, "No coding harness installed or selected in settings")
		return
	}

	prompt := fmt.Sprintf(
		"File: %s (lines %d-%d)\n\nOriginal Code:\n```\n%s\n```\n\nInstruction: %s\n\nOutput ONLY the replacement code enclosed in <replacement>...</replacement> tags. Do NOT modify any files on disk. Do NOT provide explanation or conversational filler.",
		req.Path, req.L1, req.L2, req.Selection, req.Instruction,
	)

	// Sandboxed execution in isolated temp directory
	tempDir, err := os.MkdirTemp("", "punk0-quickedit-*")
	if err != nil {
		fail(w, http.StatusInternalServerError, "failed to create sandbox")
		return
	}
	defer os.RemoveAll(tempDir)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	res := harness.Run(ctx, harness.Command{Name: harnessName, Args: baseArgv, Model: model}, prompt, harness.Options{
		Dir:     tempDir,
		Timeout: 30 * time.Second,
		Stdout:  &stdoutBuf,
		Stderr:  &stderrBuf,
	})

	if res.Err != nil {
		errMsg := res.Err.Error()
		if stderrBuf.Len() > 0 {
			errMsg += ": " + stderrBuf.String()
		}
		fail(w, http.StatusUnprocessableEntity, errMsg)
		return
	}

	replacement, err := parseQuickEditOutput(stdoutBuf.String())
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true, "replacement": replacement})
}
