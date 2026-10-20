package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var gitDisabled bool

type gitInfo struct {
	ok       bool
	toplevel string // repo root as git reports it (symlinks resolved)
	gitdir   string // absolute path to .git directory or file
}

var (
	gitMu    sync.Mutex
	gitCache = map[string]gitInfo{}
)

// gitAvailable reports whether the git binary is on PATH and root sits inside a
// working tree. Memoized per root.
func gitAvailable(root string) bool { return gitProbe(root).ok }

func gitProbe(root string) gitInfo {
	if gitDisabled {
		return gitInfo{}
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	if info, ok := gitCache[root]; ok {
		return info
	}
	var info gitInfo
	if _, err := exec.LookPath("git"); err == nil {
		if out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); err == nil {
			top := strings.TrimSpace(string(out))
			gd := filepath.Join(top, ".git")
			if gdOut, err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Output(); err == nil {
				rawGd := strings.TrimSpace(string(gdOut))
				if filepath.IsAbs(rawGd) {
					gd = rawGd
				} else {
					gd = filepath.Join(top, rawGd)
				}
			}
			info = gitInfo{ok: true, toplevel: top, gitdir: gd}
		}
	}
	gitCache[root] = info
	return info
}

func repoRelKey(info gitInfo, root string) func(string) (string, bool) {
	prefix := ""
	if rel, err := filepath.Rel(info.toplevel, root); err == nil && rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	return func(p string) (string, bool) {
		if prefix == "" {
			return p, true
		}
		if !strings.HasPrefix(p, prefix) {
			return "", false
		}
		return p[len(prefix):], true
	}
}

// gitStatusAgainst maps changed files relative to root against base.
func gitStatusAgainst(root, base string) map[string]string {
	info := gitProbe(root)
	if !info.ok {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v2", "-z", "-uall").Output()
	if err != nil {
		return nil
	}
	key := repoRelKey(info, root)

	status := map[string]string{}
	fields := strings.Split(string(out), "\x00")
	for _, f := range fields {
		if f == "" {
			continue
		}
		switch f[0] {
		case '?': // "? <path>"
			if k, ok := key(f[2:]); ok {
				status[k] = "U" // untracked
			}
		case '1': // "1 <XY> ... <path>"
			p := strings.SplitN(f, " ", 9)
			if len(p) == 9 {
				if k, ok := key(p[8]); ok {
					status[k] = mapXY(p[1])
				}
			}
		case '2': // "2 <XY> ... <path>"
			p := strings.SplitN(f, " ", 10)
			if len(p) == 10 {
				if k, ok := key(p[9]); ok {
					status[k] = mapXY(p[1])
				}
			}
		case 'u': // "u <XY> ... <path>"
			p := strings.SplitN(f, " ", 11)
			if len(p) == 11 {
				if k, ok := key(p[10]); ok {
					status[k] = "!"
				}
			}
		}
	}
	if len(status) == 0 {
		return nil
	}
	return status
}

func mapXY(xy string) string {
	if len(xy) < 2 {
		return "M"
	}
	c := xy[0]
	if c == '.' {
		c = xy[1]
	}
	switch c {
	case 'A':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	case 'C':
		return "C"
	case 'U':
		return "!"
	default:
		return "M"
	}
}

// gitStagedPaths maps path -> true for files with staged index changes.
func gitStagedPaths(root string) map[string]bool {
	info := gitProbe(root)
	if !info.ok {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "--cached", "-z").Output()
	if err != nil {
		return nil
	}
	key := repoRelKey(info, root)
	staged := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		if k, ok := key(p); ok {
			staged[k] = true
		}
	}
	if len(staged) == 0 {
		return nil
	}
	return staged
}
