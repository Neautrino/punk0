package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileEntry represents a single indexed file in the workspace.
type FileEntry struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	lower     string
	nameStart int
}

// Node represents a file or directory entry in the hierarchical file tree view.
type Node struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Ignored bool   `json:"ignored,omitempty"`
	Status  string `json:"status,omitempty"`
	Staged  bool   `json:"staged,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
}

var vcsDirs = map[string]bool{".git": true, ".hg": true, ".svn": true}

// Index maintains an in-memory representation of the workspace filesystem.
type Index struct {
	root string

	mu           sync.RWMutex
	files        []FileEntry
	children     map[string][]Node
	builtAt      time.Time
	buildMS      int64
	gitChanges   int
	gitFiles     []string
	gitStatusMap map[string]string
	gitStagedMap map[string]bool
	diffBase     string
	readyCh      chan struct{}
}

func NewIndex(root string) *Index {
	return &Index{root: root, children: map[string][]Node{}, readyCh: make(chan struct{})}
}

func (ix *Index) Root() string { return ix.root }

func (ix *Index) SetDiffBase(base string) {
	ix.mu.Lock()
	ix.diffBase = base
	ix.mu.Unlock()
}

func (ix *Index) DiffBase() string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.diffBase
}

func (ix *Index) Ready() bool {
	select {
	case <-ix.readyCh:
		return true
	default:
		return false
	}
}

func (ix *Index) WaitReady(ctx context.Context) error {
	select {
	case <-ix.readyCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ix *Index) Stats() (files int, builtAt time.Time, ms int64) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.files), ix.builtAt, ix.buildMS
}

func (ix *Index) Files() []FileEntry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	res := make([]FileEntry, len(ix.files))
	copy(res, ix.files)
	return res
}

func (ix *Index) GitChanges() (int, []string) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.gitFiles == nil {
		return 0, nil
	}
	res := make([]string, len(ix.gitFiles))
	copy(res, ix.gitFiles)
	return ix.gitChanges, res
}

func (ix *Index) GitStatusMap() map[string]string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.gitStatusMap == nil {
		return nil
	}
	res := make(map[string]string, len(ix.gitStatusMap))
	for k, v := range ix.gitStatusMap {
		res[k] = v
	}
	return res
}

func (ix *Index) GitStagedMap() map[string]bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.gitStagedMap == nil {
		return nil
	}
	res := make(map[string]bool, len(ix.gitStagedMap))
	for k, v := range ix.gitStagedMap {
		res[k] = v
	}
	return res
}

func (ix *Index) Children(dir string) ([]Node, bool) {
	ix.mu.RLock()
	c, ok := ix.children[dir]
	under := !ok && ix.underIgnoredLocked(dir)
	var res []Node
	if ok {
		res = make([]Node, len(c))
		copy(res, c)
	}
	ix.mu.RUnlock()
	if ok || !under {
		return res, ok
	}
	return ix.listIgnored(dir)
}

func (ix *Index) underIgnoredLocked(dir string) bool {
	if dir == "" || strings.ContainsRune(dir, '\\') {
		return false
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	for p := dir; p != ""; {
		parent := ""
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			parent = p[:i]
		}
		if kids, ok := ix.children[parent]; ok {
			name := strings.TrimPrefix(p[len(parent):], "/")
			for _, k := range kids {
				if k.Name == name {
					return k.Dir && k.Ignored
				}
			}
			return false
		}
		p = parent
	}
	return false
}

func (ix *Index) listIgnored(dir string) ([]Node, bool) {
	ents, err := os.ReadDir(filepath.Join(ix.root, filepath.FromSlash(dir)))
	if err != nil {
		return nil, false
	}
	kids := make([]Node, 0, len(ents))
	for _, e := range ents {
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		kids = append(kids, Node{Name: e.Name(), Path: dir + "/" + e.Name(), Dir: e.IsDir(), Ignored: true})
	}
	sortNodes(kids)
	return kids, true
}

func sortNodes(kids []Node) {
	sort.Slice(kids, func(i, j int) bool {
		if kids[i].Dir != kids[j].Dir {
			return kids[i].Dir
		}
		return strings.ToLower(kids[i].Name) < strings.ToLower(kids[j].Name)
	})
}

func (ix *Index) Build() {
	start := time.Now()
	root := newIgnoreSet(nil)
	root = root.Child(readGitignore(ix.root, ""))

	base := ix.DiffBase()
	gs := gitStatusAgainst(ix.root, base)
	staged := gitStagedPaths(ix.root)
	dirtyDirs := map[string]bool{}
	var gitFiles []string
	if gs != nil {
		for p := range gs {
			gitFiles = append(gitFiles, p)
			for i := strings.LastIndexByte(p, '/'); i >= 0; i = strings.LastIndexByte(p, '/') {
				p = p[:i]
				dirtyDirs[p] = true
			}
		}
		sort.Slice(gitFiles, func(i, j int) bool {
			si, sj := gs[gitFiles[i]], gs[gitFiles[j]]
			if (si != "U") != (sj != "U") {
				return si != "U"
			}
			return gitFiles[i] < gitFiles[j]
		})
	}

	var (
		mu       sync.Mutex
		files    []FileEntry
		children = map[string][]Node{}
		wg       sync.WaitGroup
		sem      = make(chan struct{}, runtime.NumCPU()*4)
	)

	var walk func(abs, rel string, ig *ignoreSet)
	walk = func(abs, rel string, ig *ignoreSet) {
		defer wg.Done()
		ents, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		if rel != "" {
			if extra := readGitignore(abs, rel); len(extra) > 0 {
				ig = ig.Child(extra)
			}
		}
		kids := make([]Node, 0, len(ents))
		var subdirs []struct {
			abs, rel string
		}
		for _, e := range ents {
			name := e.Name()
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			isDir := e.IsDir()
			if e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if ig.Match(childRel, isDir) {
				if !(isDir && vcsDirs[name]) {
					kids = append(kids, Node{Name: name, Path: childRel, Dir: isDir, Ignored: true})
				}
				continue
			}
			if isDir {
				kids = append(kids, Node{Name: name, Path: childRel, Dir: true, Dirty: dirtyDirs[childRel]})
				subdirs = append(subdirs, struct{ abs, rel string }{filepath.Join(abs, name), childRel})
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			kids = append(kids, Node{Name: name, Path: childRel, Size: info.Size(), Status: gs[childRel], Staged: staged[childRel]})
			mu.Lock()
			files = append(files, FileEntry{
				Path: childRel, Name: name, Size: info.Size(),
				lower: strings.ToLower(childRel), nameStart: len(childRel) - len(name),
			})
			mu.Unlock()
		}
		sortNodes(kids)
		mu.Lock()
		children[rel] = kids
		mu.Unlock()

		if rel == "" {
			ix.mu.Lock()
			if ix.children == nil {
				ix.children = map[string][]Node{}
			}
			rootKids := make([]Node, len(kids))
			copy(rootKids, kids)
			for p, st := range gs {
				if st == "D" && !strings.Contains(p, "/") {
					rootKids = append(rootKids, Node{
						Name:   p,
						Path:   p,
						Dir:    false,
						Status: "D",
						Staged: staged[p],
					})
				}
			}
			sortNodes(rootKids)
			ix.children[""] = rootKids
			ix.mu.Unlock()
		}

		for _, sd := range subdirs {
			wg.Add(1)
			select {
			case sem <- struct{}{}:
				go func(a, r string, g *ignoreSet) {
					defer func() { <-sem }()
					walk(a, r, g)
				}(sd.abs, sd.rel, ig)
			default:
				walk(sd.abs, sd.rel, ig)
			}
		}
	}

	wg.Add(1)
	walk(ix.root, "", root)
	wg.Wait()

	injectDeletedNodes(children, gs, staged, dirtyDirs)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	ix.mu.Lock()
	ix.gitChanges = len(gitFiles)
	ix.gitFiles = gitFiles
	ix.gitStatusMap = gs
	ix.gitStagedMap = staged
	ix.files, ix.children = files, children
	ix.builtAt, ix.buildMS = time.Now(), time.Since(start).Milliseconds()
	select {
	case <-ix.readyCh:
	default:
		close(ix.readyCh)
	}
	ix.mu.Unlock()
}

func injectDeletedNodes(children map[string][]Node, gs map[string]string, staged map[string]bool, dirtyDirs map[string]bool) {
	apply := func(p string, st string, isStaged bool) {
		parts := strings.Split(p, "/")
		for i := range len(parts) - 1 {
			parentDir := ""
			if i > 0 {
				parentDir = strings.Join(parts[:i], "/")
			}
			subDir := strings.Join(parts[:i+1], "/")
			name := parts[i]

			kids := children[parentDir]
			found := false
			for _, k := range kids {
				if k.Name == name && k.Dir {
					found = true
					break
				}
			}
			if !found {
				children[parentDir] = append(children[parentDir], Node{
					Name:  name,
					Path:  subDir,
					Dir:   true,
					Dirty: dirtyDirs[subDir],
				})
				sortNodes(children[parentDir])
			}
			if _, ok := children[subDir]; !ok {
				children[subDir] = []Node{}
			}
		}

		dir := ""
		name := p
		if lastSlash := strings.LastIndexByte(p, '/'); lastSlash >= 0 {
			dir = p[:lastSlash]
			name = p[lastSlash+1:]
		}

		kids := children[dir]
		found := false
		for i := range kids {
			if kids[i].Path == p && !kids[i].Dir {
				kids[i].Status = st
				kids[i].Staged = isStaged
				found = true
				break
			}
		}
		if !found {
			children[dir] = append(children[dir], Node{
				Name:   name,
				Path:   p,
				Dir:    false,
				Status: st,
				Staged: isStaged,
			})
			sortNodes(children[dir])
		}
	}

	for p, st := range gs {
		apply(p, st, staged != nil && staged[p])
	}
}

func cleanupEmptyDirs(children map[string][]Node, root string) {
	changed := true
	for changed {
		changed = false
		for dir, kids := range children {
			if dir == "" || len(kids) > 0 {
				continue
			}
			absDir := filepath.Join(root, filepath.FromSlash(dir))
			if _, err := os.Stat(absDir); err != nil {
				delete(children, dir)
				parent := ""
				if idx := strings.LastIndexByte(dir, '/'); idx >= 0 {
					parent = dir[:idx]
				}
				pkids := children[parent]
				pFiltered := pkids[:0]
				for _, k := range pkids {
					if k.Path != dir {
						pFiltered = append(pFiltered, k)
					}
				}
				children[parent] = pFiltered
				changed = true
				break
			}
		}
	}
}

func (ix *Index) UpdateGitStatus() (count int, files []string, changed bool) {
	if !ix.Ready() || gitDisabled || !gitAvailable(ix.root) {
		return 0, nil, false
	}

	base := ix.DiffBase()
	gs := gitStatusAgainst(ix.root, base)
	if gs == nil {
		gs = map[string]string{}
	}
	sg := gitStagedPaths(ix.root)
	if sg == nil {
		sg = map[string]bool{}
	}

	newDirtyDirs := map[string]bool{}
	var newGitFiles []string
	for p := range gs {
		newGitFiles = append(newGitFiles, p)
		for i := strings.LastIndexByte(p, '/'); i >= 0; i = strings.LastIndexByte(p, '/') {
			p = p[:i]
			newDirtyDirs[p] = true
		}
	}
	sort.Slice(newGitFiles, func(i, j int) bool {
		si, sj := gs[newGitFiles[i]], gs[newGitFiles[j]]
		if (si != "U") != (sj != "U") {
			return si != "U"
		}
		return newGitFiles[i] < newGitFiles[j]
	})

	ix.mu.Lock()
	defer ix.mu.Unlock()

	same := len(gs) == len(ix.gitStatusMap) && len(sg) == len(ix.gitStagedMap)
	if same {
		for k, v := range gs {
			if ix.gitStatusMap[k] != v {
				same = false
				break
			}
		}
	}
	if same {
		for k, v := range sg {
			if ix.gitStagedMap[k] != v {
				same = false
				break
			}
		}
	}
	if same {
		resFiles := make([]string, len(ix.gitFiles))
		copy(resFiles, ix.gitFiles)
		return ix.gitChanges, resFiles, false
	}

	for dir, kids := range ix.children {
		filtered := kids[:0]
		for _, node := range kids {
			if node.Dir {
				node.Dirty = newDirtyDirs[node.Path]
				filtered = append(filtered, node)
			} else {
				newStatus := gs[node.Path]
				newStaged := sg[node.Path]

				if node.Status == "D" && newStatus != "D" {
					absPath := filepath.Join(ix.root, filepath.FromSlash(node.Path))
					if _, err := os.Stat(absPath); err != nil {
						continue
					}
				}

				node.Status = newStatus
				node.Staged = newStaged
				filtered = append(filtered, node)
			}
		}
		ix.children[dir] = filtered
	}

	injectDeletedNodes(ix.children, gs, sg, newDirtyDirs)
	cleanupEmptyDirs(ix.children, ix.root)

	ix.gitChanges = len(newGitFiles)
	ix.gitFiles = newGitFiles
	ix.gitStatusMap = gs
	ix.gitStagedMap = sg

	resFiles := make([]string, len(ix.gitFiles))
	copy(resFiles, ix.gitFiles)
	return ix.gitChanges, resFiles, true
}
