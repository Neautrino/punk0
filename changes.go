package main

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Caps for /api/changes, which highlights every changed file at once: a file
// past changeFileMax is listed without its lines, and past changesMax files the
// rest are dropped (truncated in the response).
const (
	changeFileMax = 256 << 10
	changesMax    = 500
)

// ChangedFile is one file of the working tree's diff against diffBase: its
// hunks ready to draw, or why there are none (binary, large, a pure rename).
type ChangedFile struct {
	Path   string     `json:"path"`
	From   string     `json:"from,omitempty"` // previous path, for R only
	Status string     `json:"status"`         // A, M, D, R, or U for untracked
	Add    int        `json:"add"`
	Del    int        `json:"del"`
	Binary bool       `json:"binary,omitempty"`
	Large  bool       `json:"large,omitempty"`
	Hunks  []DiffHunk `json:"hunks"`
}

// handleChanges returns every changed file of the served folder with its
// hunks, for the agents page's Diff view: one request instead of one
// /api/diff per file, and untracked files, which git diff leaves out, as
// wholly added. ?path= narrows it to that one file, for a file tab showing
// its diff; an unchanged file comes back as no files.
func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	root := s.ix.Root()
	only := ""
	if p := r.URL.Query().Get("path"); p != "" {
		_, rel, ok := s.safePath(p)
		if !ok {
			fail(w, 400, "bad path")
			return
		}
		only = rel
	}
	if !gitAvailable(root) {
		writeJSON(w, map[string]any{"git": false, "files": []ChangedFile{}})
		return
	}
	files, truncated := gitChanges(root, s.diffBase, only)
	writeJSON(w, map[string]any{"git": true, "files": files, "truncated": truncated})
}

// gitChanges diffs the working tree under root against base, renames
// detected, and adds the untracked files, sorted by path. Paths are relative
// to root; changes outside it are left out. A non-empty only keeps just that
// path; the whole tree is still diffed, so a renamed file keeps its old side.
func gitChanges(root, base, only string) ([]ChangedFile, bool) {
	files := []ChangedFile{}
	out, err := exec.Command("git", "-C", root, "diff", "--no-color", "--no-ext-diff", "-M", "--relative", base).Output()
	if err == nil {
		for _, chunk := range splitDiffFiles(string(out)) {
			f := diffFileHead(chunk)
			if only == "" || f.Path == only {
				files = append(files, diffFileBody(f, chunk))
			}
		}
	}
	for _, rel := range gitUntracked(root) {
		if only == "" || rel == only {
			files = append(files, untrackedChange(root, rel))
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) > changesMax {
		return files[:changesMax], true
	}
	return files, false
}

// splitDiffFiles cuts a multi-file git diff into one chunk per file.
func splitDiffFiles(diff string) []string {
	var chunks []string
	start := -1
	for i := 0; i < len(diff); {
		if strings.HasPrefix(diff[i:], "diff --git ") {
			if start >= 0 {
				chunks = append(chunks, diff[start:i])
			}
			start = i
		}
		nl := strings.IndexByte(diff[i:], '\n')
		if nl < 0 {
			break
		}
		i += nl + 1
	}
	if start >= 0 {
		chunks = append(chunks, diff[start:])
	}
	return chunks
}

// diffFileHead reads one file's chunk of a git diff up to its hunks: its
// path and status from the extended header.
func diffFileHead(chunk string) ChangedFile {
	f := ChangedFile{Status: "M", Hunks: []DiffHunk{}}
	head, _, _ := strings.Cut(chunk, "\n@@")
	for i, line := range strings.Split(head, "\n") {
		switch {
		case i == 0:
			f.Path = diffHeaderPath(strings.TrimPrefix(line, "diff --git "))
		case strings.HasPrefix(line, "new file mode"):
			f.Status = "A"
		case strings.HasPrefix(line, "deleted file mode"):
			f.Status = "D"
		case strings.HasPrefix(line, "rename from "):
			f.Status = "R"
			f.From = gitUnquote(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			f.Path = gitUnquote(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch":
			f.Binary = true
		}
	}
	return f
}

// diffFileBody adds the chunk's hunks, highlighted, and their line counts to
// f, its header read by diffFileHead.
func diffFileBody(f ChangedFile, chunk string) ChangedFile {
	if len(chunk) > changeFileMax {
		f.Large = true
		f.Add, f.Del = countDiffLines(chunk)
		return f
	}
	if hunks := highlightDiff(f.Path, chunk); len(hunks) > 0 {
		f.Hunks = hunks
	}
	f.Add, f.Del = countRows(f.Hunks)
	return f
}

// diffHeaderPath takes the path out of "a/<path> b/<path>", the rest of a
// "diff --git" line. Both sides name the same file unless it was renamed,
// which the rename lines say instead; git quotes a path with unusual bytes.
func diffHeaderPath(rest string) string {
	if strings.HasPrefix(rest, `"`) {
		if q, err := strconv.QuotedPrefix(rest); err == nil {
			p, _ := strconv.Unquote(q)
			return strings.TrimPrefix(p, "a/")
		}
	}
	n := (len(rest) - len("a/ b/")) / 2
	if n <= 0 {
		return rest
	}
	return rest[len("a/") : len("a/")+n]
}

// gitUnquote undoes git's C-style quoting of a path, which leaves plain paths
// as they are.
func gitUnquote(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

func countRows(hunks []DiffHunk) (add, del int) {
	for _, h := range hunks {
		for _, r := range h.Rows {
			switch r.Type {
			case "add":
				add++
			case "del":
				del++
			}
		}
	}
	return add, del
}

// countDiffLines counts a chunk's added and deleted lines without parsing
// its hunks, for a file too large to show.
func countDiffLines(chunk string) (add, del int) {
	_, body, _ := strings.Cut(chunk, "\n@@")
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	return add, del
}

// untrackedChange shows an untracked file as one hunk adding all of it.
func untrackedChange(root, rel string) ChangedFile {
	f := ChangedFile{Path: rel, Status: "U", Hunks: []DiffHunk{}}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return f
	}
	if info.Size() > changeFileMax {
		f.Large = true
		return f
	}
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return f
	}
	if bytes.IndexByte(src[:min(len(src), 8000)], 0) >= 0 {
		f.Binary = true
		return f
	}
	if len(src) == 0 {
		return f
	}
	lines := strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	var b strings.Builder
	b.WriteString("@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@\n")
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	f.Hunks = highlightDiff(rel, b.String())
	f.Add = len(lines)
	return f
}
