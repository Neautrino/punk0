package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestChanges(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A space and a non-ASCII byte: git writes the first as is and quotes the second.
	write("a b.go", "one\n")
	write("ü.go", "one\n")
	git("add", "--", "a b.go", "ü.go")
	git("commit", "-qm", "names", "--", "a b.go", "ü.go")
	write("a b.go", "two\n")
	write("ü.go", "two\n")

	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/changes", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Git       bool          `json:"git"`
		Truncated bool          `json:"truncated"`
		Files     []ChangedFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Git || body.Truncated {
		t.Fatalf("git=%v truncated=%v, want true/false", body.Git, body.Truncated)
	}

	type want struct {
		status, from string
		add, del     int
		hunks        bool
	}
	wants := map[string]want{
		"a b.go":      {"M", "", 1, 1, true},
		"add.go":      {"A", "", 1, 0, true},
		"del.go":      {"D", "", 0, 1, true},
		"sub/mod.go":  {"M", "", 1, 1, true},
		"sub/ren2.go": {"R", "sub/ren.go", 0, 0, false},
		"untr.go":     {"U", "", 1, 0, true},
		"ü.go":        {"M", "", 1, 1, true},
	}
	var paths []string
	for _, f := range body.Files {
		paths = append(paths, f.Path)
		w, ok := wants[f.Path]
		if !ok {
			t.Errorf("unexpected changed file %q (%+v)", f.Path, f)
			continue
		}
		if f.Status != w.status || f.From != w.from || f.Add != w.add || f.Del != w.del || (len(f.Hunks) > 0) != w.hunks {
			t.Errorf("%s = {status %q from %q +%d -%d hunks %d}, want %+v", f.Path, f.Status, f.From, f.Add, f.Del, len(f.Hunks), w)
		}
	}
	if len(paths) != len(wants) {
		t.Errorf("files = %q, want %d of them", paths, len(wants))
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1] > paths[i] {
			t.Errorf("files not sorted by path: %q", paths)
			break
		}
	}

	// The untracked file is one hunk adding its lines, highlighted.
	for _, f := range body.Files {
		if f.Path != "untr.go" {
			continue
		}
		r := f.Hunks[0].Rows[0]
		if f.Hunks[0].NewStart != 1 || r.Type != "add" || r.NewLine != 1 || r.Text != "untracked" || r.HTML == "" {
			t.Errorf("untracked hunk = %+v, want one added line 1 'untracked' with HTML", f.Hunks[0])
		}
	}

	// ?path= keeps just that file: a rename still with its old side, an
	// untracked file, and nothing for an unchanged one.
	for _, c := range []struct{ path, status, from string }{
		{"sub/ren2.go", "R", "sub/ren.go"},
		{"untr.go", "U", ""},
		{"sub/ren.go", "", ""},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/changes?path="+url.QueryEscape(c.path), nil))
		var one struct {
			Files []ChangedFile `json:"files"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil || rec.Code != 200 {
			t.Fatalf("path %s: status %d: %s", c.path, rec.Code, rec.Body)
		}
		switch {
		case c.status == "" && len(one.Files) != 0:
			t.Errorf("path %s: files %+v, want none", c.path, one.Files)
		case c.status != "" && (len(one.Files) != 1 || one.Files[0].Path != c.path || one.Files[0].Status != c.status || one.Files[0].From != c.from):
			t.Errorf("path %s: files %+v, want one, status %s from %q", c.path, one.Files, c.status, c.from)
		}
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/changes?path=../x", nil))
	if rec.Code != 400 {
		t.Errorf("path ../x: status %d, want 400", rec.Code)
	}
}

func TestChangesWithoutGit(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("a\n"), 0o644)
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/changes", nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if files, ok := body["files"].([]any); rec.Code != 200 || body["git"] != false || !ok || len(files) != 0 {
		t.Errorf("got %d %s, want 200 with git false and an empty files list", rec.Code, rec.Body)
	}
}
