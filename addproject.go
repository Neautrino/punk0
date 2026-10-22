package main

// The Add project panel's three ways in, beside adding a folder (POST
// /api/projects): describing the folder the panel is on, making a new
// project, and cloning one. A clone runs in the background, outside every
// lock, and is polled for its progress; it clones into a hidden folder beside
// its destination and is renamed into place, so a cancelled or failed clone
// leaves nothing that looks like a project.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// handleFolder describes the folder the Add project panel is on: whether it
// exists, the repository it is in, and the project it already is.
func (wb *workbench) handleFolder(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	dir, err := expandPath(r.URL.Query().Get("dir"), wb.cwd)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{"dir": dir, "exists": isDir(dir)}
	if !isDir(dir) {
		writeJSON(w, out)
		return
	}
	key := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		key = resolved
	}
	if info := gitProbe(key); info.ok {
		out["git"], out["top"], out["branch"], out["empty"] = true, info.toplevel, gitBranch(key), !hasCommit(key)
		key = info.toplevel
	}
	out["project"] = wb.records.ProjectAt(key)
	writeJSON(w, out)
}

func (wb *workbench) handleRemovedProjects(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	writeJSON(w, wb.records.Removed())
}

// handleProjectRefresh makes punk0 look at a project folder afresh, for
// changes made outside it (git init, a new branch): git is asked again and
// the folder's editor restarts on the next request, since an editor reads
// whether its folder is a repository when it starts.
func (wb *workbench) handleProjectRefresh(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	dir := wb.records.ProjectDir(r.PathValue("id"))
	if dir == "" {
		fail(w, http.StatusNotFound, "project not found")
		return
	}
	gitForget(dir)
	wb.stop(wbItemID("dir", dir))
	writeJSON(w, map[string]any{"ok": true})
}

/* ---------- a new project ---------- */

// handleProjectNew makes a folder in parent and runs git init in it, nothing
// more: the first commit is the user's.
func (wb *workbench) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := gitUsable(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := folderLabel(body.Name)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	dest, err := wb.newFolderIn(body.Parent, name)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if out, err := exec.Command("git", "-C", dest, "init", "-q").CombinedOutput(); err != nil {
		os.RemoveAll(dest)
		fail(w, http.StatusInternalServerError, "git init: "+gitError(out, err).Error())
		return
	}
	wb.openFolder(w, dest)
}

// gitUsable says why git cannot be run, or nil.
func gitUsable() error {
	if gitDisabled {
		return errors.New("git is required for this: restart punk0 without -no-git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is required for this and is not installed")
	}
	return nil
}

// folderLabel checks a folder name the user typed for a new project.
func folderLabel(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("name the project")
	case name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", fmt.Errorf("%q cannot be a folder name", name)
	case strings.HasPrefix(name, "-"):
		return "", fmt.Errorf("%q cannot start with -", name)
	case len(name) > 100:
		return "", errors.New("the name is too long")
	}
	return name, nil
}

// newFolderIn is parent/name, when parent is an existing folder and nothing
// is at parent/name yet.
func (wb *workbench) newFolderIn(parent, name string) (string, error) {
	if strings.TrimSpace(parent) == "" {
		return "", errors.New("choose the folder it goes in")
	}
	p, err := expandPath(parent, wb.cwd)
	if err != nil {
		return "", err
	}
	if !isDir(p) {
		return "", fmt.Errorf("%s is not a folder", tildePath(p))
	}
	dest := filepath.Join(p, name)
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("%s already exists: add it as a folder instead", tildePath(dest))
	}
	return dest, nil
}

/* ---------- cloning ---------- */

// cloneJob is one clone under way, or just finished.
type cloneJob struct {
	id     string
	dir    string // where it ends up
	cancel context.CancelFunc

	mu        sync.Mutex
	phase     string // git's current step, e.g. "Receiving objects"
	percent   int
	tail      []byte // the last of git's output, for the error
	done      bool
	err       string
	project   string
	workspace string
}

// cloneDoneTTL is how long a finished clone stays to be polled.
const cloneDoneTTL = time.Minute

// gitProgress matches one progress line of git clone --progress.
var gitProgress = regexp.MustCompile(`^(?:remote: )?([A-Za-z][A-Za-z ]*?):\s+(\d+)%`)

// Write takes git's output: progress lines end in \r while they update and
// in \n when done.
func (j *cloneJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.tail = append(j.tail, p...)
	if len(j.tail) > 8<<10 {
		j.tail = j.tail[len(j.tail)-4<<10:]
	}
	lines := strings.FieldsFunc(string(j.tail), func(r rune) bool { return r == '\r' || r == '\n' })
	for i := len(lines) - 1; i >= 0; i-- {
		if m := gitProgress.FindStringSubmatch(lines[i]); m != nil {
			j.phase = m[1]
			j.percent, _ = strconv.Atoi(m[2])
			break
		}
	}
	return len(p), nil
}

// failure is git's own reason for failing: its last fatal or error line.
func (j *cloneJob) failure(err error) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	lines := strings.FieldsFunc(string(j.tail), func(r rune) bool { return r == '\r' || r == '\n' })
	for i := len(lines) - 1; i >= 0; i-- {
		if msg, ok := strings.CutPrefix(lines[i], "fatal: "); ok {
			return msg
		}
		if msg, ok := strings.CutPrefix(lines[i], "error: "); ok {
			return msg
		}
	}
	return err.Error()
}

func (j *cloneJob) view() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return map[string]any{
		"id": j.id, "dir": tildePath(j.dir), "phase": j.phase, "percent": j.percent,
		"done": j.done, "error": j.err, "project": j.project, "workspace": j.workspace,
	}
}

func (j *cloneJob) finish(err, project, workspace string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done, j.err, j.project, j.workspace = true, err, project, workspace
}

// repoFolderName is the folder a clone of url gets, as git would name it:
// the last part of the URL without .git.
func repoFolderName(url string) string {
	u := strings.TrimRight(strings.TrimSpace(url), `/\`)
	u = strings.TrimRight(strings.TrimSuffix(u, ".git"), `/\`) // repo/.git is repo
	if i := strings.LastIndexAny(u, `/\:`); i >= 0 {
		u = u[i+1:]
	}
	if u == "" || u == "." || u == ".." || strings.HasPrefix(u, "-") {
		return ""
	}
	return u
}

// handleCloneStart starts cloning url into a new folder in parent and answers
// at once; GET /api/clones/{id} follows it.
func (wb *workbench) handleCloneStart(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		URL    string `json:"url"`
		Parent string `json:"parent"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := gitUsable(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	url := strings.TrimSpace(body.URL)
	if url == "" {
		fail(w, http.StatusBadRequest, "paste the repository's URL")
		return
	}
	if _, _, ok := DetectPRURL(url); ok {
		fail(w, http.StatusBadRequest, "that is a pull request: paste it in the folder field above to review it")
		return
	}
	name := repoFolderName(url)
	if strings.HasPrefix(url, "-") || name == "" {
		fail(w, http.StatusBadRequest, "not a repository URL")
		return
	}
	dest, err := wb.newFolderIn(body.Parent, name)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	wb.mu.Lock()
	if wb.closing {
		wb.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, "punk0 is stopping")
		return
	}
	for _, j := range wb.clones {
		if j.dir == dest && !j.isDone() {
			wb.mu.Unlock()
			fail(w, http.StatusConflict, tildePath(dest)+" is being cloned already")
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &cloneJob{id: newID(), dir: dest, cancel: cancel}
	if wb.clones == nil {
		wb.clones = map[string]*cloneJob{}
	}
	wb.clones[j.id] = j
	wb.cloning.Add(1) // under wb.mu, so Close cannot miss it
	wb.mu.Unlock()

	go func() {
		defer wb.cloning.Done()
		wb.runClone(ctx, j, url)
	}()
	writeJSON(w, j.view())
}

func (j *cloneJob) isDone() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

// runClone clones beside the destination and renames the clone into place,
// then adds it as a project.
func (wb *workbench) runClone(ctx context.Context, j *cloneJob, url string) {
	defer j.cancel()
	// Polled a while longer, then forgotten.
	defer func() {
		time.AfterFunc(cloneDoneTTL, func() {
			wb.mu.Lock()
			delete(wb.clones, j.id)
			wb.mu.Unlock()
		})
	}()
	tmp, err := os.MkdirTemp(filepath.Dir(j.dir), "."+filepath.Base(j.dir)+"-clone-*")
	if err != nil {
		j.finish(err.Error(), "", "")
		return
	}
	defer os.RemoveAll(tmp) // gone by now when the clone succeeded
	// A partial clone: every file is checked out, older contents download
	// when something asks for them.
	cmd := gitAuthCmd(gitAuth{}, "clone", "--progress", "--filter=blob:none", "--", url, tmp)
	if err := runCtxTo(ctx, cmd, j); err != nil {
		if ctx.Err() != nil {
			j.finish("cancelled", "", "")
			return
		}
		j.finish(j.failure(err), "", "")
		return
	}
	if _, err := os.Lstat(j.dir); err == nil {
		j.finish(tildePath(j.dir)+" appeared while cloning; the clone was dropped", "", "")
		return
	}
	if err := os.Rename(tmp, j.dir); err != nil {
		j.finish(err.Error(), "", "")
		return
	}
	p, ws, err := wb.records.Open(j.dir)
	if err != nil {
		j.finish(err.Error(), "", "")
		return
	}
	j.finish("", p.ID, ws.ID)
}

func (wb *workbench) clone(id string) *cloneJob {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return wb.clones[id]
}

func (wb *workbench) handleCloneStatus(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	j := wb.clone(r.PathValue("id"))
	if j == nil {
		fail(w, http.StatusNotFound, "no such clone")
		return
	}
	writeJSON(w, j.view())
}

// handleCloneCancel stops a clone; its half-made folder is deleted.
func (wb *workbench) handleCloneCancel(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	j := wb.clone(r.PathValue("id"))
	if j == nil {
		fail(w, http.StatusNotFound, "no such clone")
		return
	}
	j.cancel()
	writeJSON(w, map[string]any{"ok": true})
}

// cancelClones stops every clone and waits for each to delete its half-made
// folder, for shutdown: git runs in its own process group, so it would
// outlive punk0.
func (wb *workbench) cancelClones() {
	wb.mu.Lock()
	for _, j := range wb.clones {
		j.cancel()
	}
	wb.mu.Unlock()
	wb.cloning.Wait()
}
