package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// primaryID names the satellite for -dir itself. Created satellites cannot
// take it: their names must not collide with an existing id.
const primaryID = "default"

// satellitesDir holds created worktrees, relative to the repository root.
// excludeSatellites keeps it out of git status.
const satellitesDir = ".punk0/satellites"

// satName is a satellite's id and directory name. The leading letter or digit
// rules out ".", ".." and option-like names.
var satName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Satellite is one worktree with its own shell and file index, both started
// on first use so an idle satellite costs no processes or memory.
type Satellite struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	Dir     string `json:"dir"`
	Primary bool   `json:"primary"`

	mu sync.Mutex
	sh *Shell
	ix *Index
}

// Shell returns the satellite's running shell, starting one if there is none
// or the last one exited.
func (s *Satellite) Shell() (*Shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh == nil || s.sh.Exited() {
		sh, err := startShell(s.Dir)
		if err != nil {
			return nil, err
		}
		s.sh = sh
	}
	return s.sh, nil
}

func (s *Satellite) Index() *Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ix == nil {
		s.ix = NewIndex(s.Dir)
		go s.ix.Build()
	}
	return s.ix
}

func (s *Satellite) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh != nil {
		s.sh.Close()
		s.sh = nil
	}
}

// Registry holds the satellites of the repository punk0 was started in.
type Registry struct {
	root string // git toplevel; empty when -dir is not in a repository

	// mu also serializes worktree add and remove, so two requests cannot
	// race for one name.
	mu   sync.Mutex
	sats []*Satellite // primary first, then in creation order
}

// NewRegistry registers dir as the primary satellite plus every worktree
// already under satellitesDir, so satellites survive a punk0 restart.
func NewRegistry(dir string) *Registry {
	primary := &Satellite{ID: primaryID, Name: filepath.Base(dir), Dir: dir, Primary: true}
	r := &Registry{sats: []*Satellite{primary}}
	if info := gitProbe(dir); info.ok {
		r.root = info.toplevel
		primary.Branch = gitBranch(dir)
		if err := excludeSatellites(dir); err != nil {
			log.Printf("exclude %s: %v", satellitesDir, err)
		}
		r.sats = append(r.sats, listSatellites(r.root)...)
	}
	primary.Index() // the view the UI opens first
	return r
}

func (r *Registry) List() []*Satellite {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sats)
}

func (r *Registry) Get(id string) *Satellite {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := r.indexLocked(id); i >= 0 {
		return r.sats[i]
	}
	return nil
}

// Create adds a worktree for branch under satellitesDir/name. A branch that
// does not exist yet is created from the primary worktree's HEAD.
func (r *Registry) Create(name, branch string) (*Satellite, error) {
	if !satName.MatchString(name) {
		return nil, fmt.Errorf("invalid name %q: use letters, digits, '.', '_' and '-'", name)
	}
	if branch == "" {
		branch = name
	}
	if r.root == "" {
		return nil, errors.New("satellites need a git repository")
	}
	if out, err := exec.Command("git", "-C", r.root, "check-ref-format", "--branch", branch).CombinedOutput(); err != nil {
		return nil, gitError(out, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexLocked(name) >= 0 {
		return nil, fmt.Errorf("satellite %q already exists", name)
	}
	dir := filepath.Join(r.root, satellitesDir, name)
	args := []string{"-C", r.root, "worktree", "add"}
	if exec.Command("git", "-C", r.root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
		args = append(args, dir, branch)
	} else {
		args = append(args, "-b", branch, dir)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return nil, gitError(out, err)
	}
	s := &Satellite{ID: name, Name: name, Branch: branch, Dir: dir}
	r.sats = append(r.sats, s)
	return s, nil
}

// Remove stops the satellite's shell and deletes its worktree, uncommitted
// changes included. The branch stays.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexLocked(id)
	if i < 0 {
		return fmt.Errorf("satellite %q not found", id)
	}
	s := r.sats[i]
	if s.Primary {
		return errors.New("the primary satellite cannot be removed")
	}
	s.close()
	if out, err := exec.Command("git", "-C", r.root, "worktree", "remove", "--force", s.Dir).CombinedOutput(); err != nil {
		return gitError(out, err)
	}
	r.sats = slices.Delete(r.sats, i, i+1)
	return nil
}

// Close stops every shell in parallel, so shutdown waits for the slowest
// hangup instead of their sum.
func (r *Registry) Close() {
	var wg sync.WaitGroup
	for _, s := range r.List() {
		wg.Go(s.close)
	}
	wg.Wait()
}

func (r *Registry) indexLocked(id string) int {
	return slices.IndexFunc(r.sats, func(s *Satellite) bool { return s.ID == id })
}

// satelliteOf resolves the {id} path segment, answering 404 when it is unknown.
func satelliteOf(reg *Registry, w http.ResponseWriter, r *http.Request) *Satellite {
	sat := reg.Get(r.PathValue("id"))
	if sat == nil {
		failJSON(w, http.StatusNotFound, "satellite not found")
	}
	return sat
}

// listSatellites returns the worktrees git knows under satellitesDir.
func listSatellites(root string) []*Satellite {
	out, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	prefix := filepath.Join(root, satellitesDir) + string(filepath.Separator)
	var sats []*Satellite
	var cur *Satellite
	for _, line := range strings.Split(string(out), "\n") {
		if dir, ok := strings.CutPrefix(line, "worktree "); ok {
			cur = nil
			if name, ok := strings.CutPrefix(dir, prefix); ok && satName.MatchString(name) && name != primaryID {
				cur = &Satellite{ID: name, Name: name, Dir: dir}
				sats = append(sats, cur)
			}
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && cur != nil {
			cur.Branch = b
		}
	}
	return sats
}

// excludeSatellites adds satellitesDir's parent to the repository's
// info/exclude, which unlike .gitignore is never committed. --git-path finds
// the shared file even when dir is itself a linked worktree.
func excludeSatellites(dir string) error {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	const pattern = "/.punk0/"
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == pattern {
				f.Close()
				return nil
			}
		}
		f.Close()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("\n" + pattern + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitError prefers git's own message, which says why (branch checked out
// elsewhere, invalid name), over the bare exit status.
func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(strings.TrimPrefix(msg, "fatal: "))
	}
	return err
}
