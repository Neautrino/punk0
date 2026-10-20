package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSatelliteLifecycle(t *testing.T) {
	repo := gitRepo(t)
	reg := NewRegistry(repo)
	defer reg.Close()

	sat, err := reg.Create("feat-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if sat.Branch != "feat-a" || git(t, sat.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "feat-a" {
		t.Fatalf("worktree not on branch feat-a: %+v", sat)
	}
	if st := git(t, repo, "status", "--porcelain"); st != "" {
		t.Fatalf("satellite dirtied the primary worktree:\n%s", st)
	}

	// A restarted punk0 finds the satellite again.
	again := NewRegistry(repo)
	if s := again.Get("feat-a"); s == nil || s.Branch != "feat-a" || s.Dir != sat.Dir {
		t.Fatalf("satellite not rediscovered: %+v", s)
	}

	if err := reg.Remove(primaryID); err == nil {
		t.Fatal("removed the primary satellite")
	}
	os.WriteFile(filepath.Join(sat.Dir, "wip.txt"), []byte("agent output"), 0o644)
	if err := reg.Remove("feat-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sat.Dir); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	if reg.Get("feat-a") != nil {
		t.Fatal("removed satellite still listed")
	}
	git(t, repo, "rev-parse", "--verify", "refs/heads/feat-a") // branch survives
}

func TestSatelliteCreateRejects(t *testing.T) {
	repo := gitRepo(t)
	reg := NewRegistry(repo)
	defer reg.Close()
	if _, err := reg.Create("taken", ""); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "branch", "used")
	git(t, repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "elsewhere"), "used")

	for _, tc := range []struct{ name, branch string }{
		{"..", ""},         // escapes satellitesDir
		{"a/b", ""},        // nested directory
		{"-x", ""},         // option-like
		{"", "feat"},       // no name
		{primaryID, ""},    // shadows the primary
		{"taken", "other"}, // duplicate name
		{"ok", "bad..ref"}, // invalid branch
		{"ok", "used"},     // branch checked out in another worktree
	} {
		if _, err := reg.Create(tc.name, tc.branch); err == nil {
			t.Errorf("Create(%q, %q) succeeded", tc.name, tc.branch)
		}
	}
	if n := len(reg.List()); n != 2 {
		t.Fatalf("got %d satellites, want primary + taken", n)
	}
}

func TestSatelliteNeedsRepo(t *testing.T) {
	reg := NewRegistry(t.TempDir())
	defer reg.Close()
	if _, err := reg.Create("x", ""); err == nil {
		t.Fatal("created a satellite outside a git repository")
	}
}
