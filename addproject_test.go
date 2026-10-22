package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A folder added before git init becomes a repository as soon as it is one,
// without a restart.
func TestGitAvailableNoticesLateInit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	old := gitMissTTL
	gitMissTTL = 0
	t.Cleanup(func() { gitMissTTL = old })
	dir := t.TempDir()
	if gitAvailable(dir) {
		t.Fatal("an empty folder is a repository")
	}
	runGit(t, dir, "init", "-q")
	if !gitAvailable(dir) {
		t.Fatal("git init went unnoticed")
	}
}

func decode(t *testing.T, w interface{ Bytes() []byte }, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Bytes(), v); err != nil {
		t.Fatalf("%v: %s", err, w.Bytes())
	}
}

// Create new makes a repository with no commits; it cannot have worktrees
// until it gets one, and the plan says so before anything is tried.
func TestNewProjectIsAnEmptyRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	parent := t.TempDir()

	w := postJSON(t, wb, "/api/projects/new", `{"name":"fresh","parent":"`+parent+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var made struct{ Project, Workspace string }
	decode(t, w.Body, &made)
	if _, err := os.Stat(filepath.Join(parent, "fresh", ".git")); err != nil {
		t.Fatalf("no repository made: %v", err)
	}
	if hasCommit(filepath.Join(parent, "fresh")) {
		t.Fatal("create made a commit")
	}
	// The sidebar shows the branch even before its first commit.
	if b := gitBranch(filepath.Join(parent, "fresh")); b == "" || b == "HEAD" {
		t.Fatalf("branch of a repository with no commits: %q", b)
	}
	w = reqJSON(t, wb, "GET", "/api/projects/"+made.Project+"/plan?name=feat&worktree=1", "")
	if !strings.Contains(w.Body.String(), "no commits yet") {
		t.Fatalf("plan for a worktree of an empty repository: %s", w.Body.String())
	}
	var refs struct{ Empty bool }
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+made.Project+"/refs", "").Body, &refs)
	if !refs.Empty {
		t.Fatal("refs does not say the repository is empty")
	}

	for _, body := range []string{
		`{"name":"fresh","parent":"` + parent + `"}`, // exists
		`{"name":"a/b","parent":"` + parent + `"}`,
		`{"name":"..","parent":"` + parent + `"}`,
		`{"name":"x","parent":"` + filepath.Join(parent, "nope") + `"}`,
	} {
		if w := postJSON(t, wb, "/api/projects/new", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
}

type cloneState struct {
	Done      bool
	Error     string
	Project   string
	Workspace string
}

func waitClone(t *testing.T, wb *workbench, id string) cloneState {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var st cloneState
		decode(t, reqJSON(t, wb, "GET", "/api/clones/"+id, "").Body, &st)
		if st.Done {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("clone never finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A clone lands in a folder named after the repository and becomes a project;
// a failed one leaves nothing behind in the folder it was going to.
func TestCloneAddsTheProject(t *testing.T) {
	src := cleanRepo(t)
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	parent := t.TempDir()

	w := postJSON(t, wb, "/api/clones", `{"url":"`+src+`/.git","parent":"`+parent+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clone: %d %s", w.Code, w.Body.String())
	}
	var job struct{ ID string }
	decode(t, w.Body, &job)
	st := waitClone(t, wb, job.ID)
	if st.Error != "" || st.Project == "" || st.Workspace == "" {
		t.Fatalf("clone finished as %+v", st)
	}
	dest := filepath.Join(parent, filepath.Base(src))
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("clone has no checkout: %v", err)
	}
	if got := wb.records.ProjectAt(dest); got != st.Project {
		t.Fatalf("project at %s is %q, want %q", dest, got, st.Project)
	}
	if w := postJSON(t, wb, "/api/clones", `{"url":"`+src+`","parent":"`+parent+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("cloned over an existing folder: %d %s", w.Code, w.Body.String())
	}

	empty := t.TempDir()
	w = postJSON(t, wb, "/api/clones", `{"url":"`+filepath.Join(empty, "missing")+`","parent":"`+empty+`"}`)
	decode(t, w.Body, &job)
	if st := waitClone(t, wb, job.ID); st.Error == "" {
		t.Fatal("a clone of nothing succeeded")
	}
	if ents, _ := os.ReadDir(empty); len(ents) != 0 {
		t.Fatalf("a failed clone left %v", ents)
	}
}

// A removed project is offered again with the worktrees it would bring back,
// across restarts, until it is added again.
func TestRemovedProjectsComeBack(t *testing.T) {
	repo := cleanRepo(t)
	home := t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := addWorkspace(rec, p.ID, "feat", true); err != nil {
		t.Fatal(err)
	}
	if err := rec.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}

	removed := openRecords(t, home).Removed()
	if len(removed) != 1 || removed[0].Dir != repo || removed[0].Worktrees != 1 {
		t.Fatalf("removed after a restart: %+v", removed)
	}
	if _, err := rec.AddProject(repo); err != nil {
		t.Fatal(err)
	}
	if removed := rec.Removed(); len(removed) != 0 {
		t.Fatalf("still offered after adding it again: %+v", removed)
	}
}
