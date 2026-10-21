package main

import (
	"runtime"
	"sort"
	"strings"
	"sync"
)

// FuzzyResult is an indexed path ranked by FuzzyFind.
type FuzzyResult struct {
	Path  string `json:"path"` // workspace-relative path
	Name  string `json:"name"` // basename
	Pos   []int  `json:"pos"`  // byte offsets in Path that matched, for highlighting
	Score int    `json:"-"`    // match quality, higher is better
}

// isBoundary reports whether a byte starts a new word or segment in a path.
func isBoundary(b byte) bool {
	switch b {
	case '/', '_', '-', '.', ' ', '@':
		return true
	}
	return false
}

// fuzzyScore does a two-pass match: forward to prove every query byte is
// present, then backward from that endpoint to pull the matched positions as
// tightly together as possible. Tight matches score higher, which is what makes
// "fzf feel" work without an O(n*m) dynamic program.
func fuzzyScore(q, origQ string, e *FileEntry, pos []int) (int, []int, bool) {
	p, lp := e.Path, e.lower
	qi, end := 0, -1
	for i := 0; i < len(lp) && qi < len(q); i++ {
		if lp[i] == q[qi] {
			qi++
			end = i
		}
	}
	if qi < len(q) {
		return 0, nil, false
	}

	pos = pos[:0]
	qi = len(q) - 1
	for i := end; i >= 0 && qi >= 0; i-- {
		if lp[i] == q[qi] {
			pos = append(pos, i)
			qi--
		}
	}
	// Collected right-to-left; flip in place.
	for i, j := 0, len(pos)-1; i < j; i, j = i+1, j-1 {
		pos[i], pos[j] = pos[j], pos[i]
	}

	score, prev := 0, -2
	for k, i := range pos {
		if i == prev+1 {
			score += 12 // consecutive run
		} else if k > 0 {
			score -= min(i-prev, 12) // gap penalty, bounded
		}
		if i >= e.nameStart {
			score += 14 // basename beats directory noise
		}
		if i == 0 || isBoundary(p[i-1]) {
			score += 16 // start of a path or word segment
		} else if p[i] >= 'A' && p[i] <= 'Z' && p[i-1] >= 'a' && p[i-1] <= 'z' {
			score += 14 // camelCase hump
		}
		if k < len(origQ) && p[i] == origQ[k] {
			score += 4 // exact case
		}
		prev = i
	}
	// Prefer the shallower, shorter of two otherwise-equal paths.
	score -= len(p) / 8
	score -= strings.Count(p, "/") * 2
	if e.nameStart <= len(lp) {
		if idx := strings.Index(lp[e.nameStart:], q); idx >= 0 {
			score += 40 // whole query appears verbatim in the basename
			if idx == 0 {
				score += 20
			}
		}
	}
	return score, pos, true
}

// FuzzyFind ranks every indexed path against query and returns the best
// limit. An empty query returns the first limit paths in index order.
func FuzzyFind(files []FileEntry, query string, limit int) []FuzzyResult {
	origQ := strings.ReplaceAll(strings.TrimSpace(query), " ", "")
	q := strings.ToLower(origQ)

	if q == "" {
		out := make([]FuzzyResult, 0, min(limit, len(files)))
		for i := range files {
			if i == limit {
				break
			}
			out = append(out, FuzzyResult{Path: files[i].Path, Name: files[i].Name, Pos: []int{}})
		}
		return out
	}

	workers := runtime.NumCPU()
	chunk := max((len(files)+workers-1)/workers, 1)
	parts := make([][]FuzzyResult, workers)
	var wg sync.WaitGroup
	for w := range workers {
		lo := w * chunk
		if lo >= len(files) {
			break
		}
		hi := min(lo+chunk, len(files))
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]FuzzyResult, 0, 64)
			scratch := make([]int, 0, 64)
			for i := lo; i < hi; i++ {
				s, pos, ok := fuzzyScore(q, origQ, &files[i], scratch)
				if !ok {
					continue
				}
				local = append(local, FuzzyResult{Path: files[i].Path, Name: files[i].Name, Pos: append([]int(nil), pos...), Score: s})
			}
			parts[w] = local
		}()
	}
	wg.Wait()

	all := make([]FuzzyResult, 0)
	for _, p := range parts {
		all = append(all, p...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].Path < all[j].Path
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}
