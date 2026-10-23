package main

// GET /api/resources: what punk0 costs right now, for the status bar's
// total and the resource panel it opens. Every figure is a process tree from
// one process-table snapshot: a terminal is its shell and everything running
// in it (an agent and the tools it starts), an editor is its punk0 child
// alone, and its language servers are everything below that child. "self" is
// the parent plus anything below it that no row claims (git, a child still
// starting), so the rows add up to the total.

import (
	"net/http"
	"os"
	"sync"
	"time"

	"px0/internal/metrics"
)

// usage is one row: CPU in percent of one core, RSS in bytes.
type usage struct {
	CPU float64 `json:"cpu"`
	RSS uint64  `json:"rss"`
}

type resTab struct {
	ID string `json:"id"`
	usage
}

// resWorkspace is a workspace with an editor or a terminal running; usage is
// their sum. Editor is its folder's child process alone, nil when the folder
// has no running child. LSP is everything below that child: its language
// servers, plus a git command now and then (short-lived, so a poll rarely
// catches one); nil with Editor.
type resWorkspace struct {
	ID     string   `json:"id"`
	Editor *usage   `json:"editor"`
	LSP    *usage   `json:"lsp"`
	Tabs   []resTab `json:"tabs"`
	usage
}

// resItem is a running child no workspace claims: a pull request review.
type resItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	usage
}

type resources struct {
	usage
	Self       usage          `json:"self"`
	Workspaces []resWorkspace `json:"workspaces"`
	Reviews    []resItem      `json:"reviews"`
}

func (wb *workbench) handleResources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, wb.resources())
}

func (wb *workbench) resources() resources {
	type child struct {
		id, title string
		pid       int
	}
	var children []child
	wb.mu.Lock()
	for id, c := range wb.children {
		if c.state == "running" && c.cmd != nil && c.cmd.Process != nil {
			children = append(children, child{id, c.title, c.cmd.Process.Pid})
		}
	}
	wb.mu.Unlock()
	running := wb.records.Running()

	out := resources{Workspaces: []resWorkspace{}, Reviews: []resItem{}}
	table := metrics.Processes()
	if table == nil { // no process table here: only the parent can be measured
		m := getProcessMetrics(nil)
		out.Self = usage{CPU: m.CPUUsage, RSS: m.RSSBytes}
		out.usage = out.Self
		return out
	}

	// Measure every tree first, then turn CPU times into percentages in one
	// pass, so all rows share one interval.
	cpuTimes := map[string]time.Duration{}
	rss := map[string]uint64{}
	measure := func(key string, pid int) {
		p := table.Tree(pid)
		cpuTimes[key], rss[key] = p.CPU, p.RSS
	}
	// An editor's tree splits in two rows: its own process, and everything
	// below it (its language servers). Together they are the tree, so the
	// claimed sum below stays a sum of disjoint trees.
	measureEditor := func(id string, pid int) {
		tree, own := table.Tree(pid), table[pid]
		cpuTimes["editor:"+id], rss["editor:"+id] = own.CPU, own.RSS
		cpuTimes["lsp:"+id], rss["lsp:"+id] = tree.CPU-own.CPU, tree.RSS-own.RSS
	}
	all := table.Tree(os.Getpid())
	cpuTimes["total"], rss["total"] = all.CPU, all.RSS

	editorOf := map[string]int{} // child id -> pid, until a workspace claims it
	for _, c := range children {
		editorOf[c.id] = c.pid
	}
	type wsKeys struct {
		ws     runningWorkspace
		editor string // child id, "" without a running editor
	}
	var shown []wsKeys
	for _, rw := range running {
		id := wbItemID("dir", rw.dir)
		editor := ""
		if pid, ok := editorOf[id]; ok {
			delete(editorOf, id) // local workspaces of a project share one child
			editor = id
			measureEditor(id, pid)
		}
		for _, t := range rw.tabs {
			measure("tab:"+t.id, t.pid)
		}
		if editor != "" || len(rw.tabs) > 0 {
			shown = append(shown, wsKeys{rw, editor})
		}
	}
	for _, c := range children {
		if _, ok := editorOf[c.id]; ok {
			measure("item:"+c.id, c.pid)
		}
	}
	var claimedCPU time.Duration
	var claimedRSS uint64
	for k, v := range cpuTimes {
		if k != "total" {
			claimedCPU += v
			claimedRSS += rss[k]
		}
	}
	// The claimed trees all sit below the parent, so they fit in the total.
	cpuTimes["self"], rss["self"] = 0, 0
	if all.CPU > claimedCPU {
		cpuTimes["self"] = all.CPU - claimedCPU
	}
	if all.RSS > claimedRSS {
		rss["self"] = all.RSS - claimedRSS
	}

	pct := wb.cpu.sample(time.Now(), cpuTimes)
	row := func(key string) usage { return usage{CPU: pct[key], RSS: rss[key]} }

	out.usage, out.Self = row("total"), row("self")
	for _, s := range shown {
		ws := resWorkspace{ID: s.ws.id, Tabs: []resTab{}}
		if s.editor != "" {
			e, l := row("editor:"+s.editor), row("lsp:"+s.editor)
			ws.Editor, ws.LSP = &e, &l
			ws.CPU, ws.RSS = e.CPU+l.CPU, e.RSS+l.RSS
		}
		for _, t := range s.ws.tabs {
			u := row("tab:" + t.id)
			ws.Tabs = append(ws.Tabs, resTab{t.id, u})
			ws.CPU += u.CPU
			ws.RSS += u.RSS
		}
		out.Workspaces = append(out.Workspaces, ws)
	}
	for _, c := range children {
		if _, ok := editorOf[c.id]; ok {
			out.Reviews = append(out.Reviews, resItem{c.id, c.title, row("item:" + c.id)})
		}
	}
	return out
}

// cpuMeter turns cumulative CPU times into percentages from one request to
// the next. Requests closer together than minInterval reuse the last figures:
// two open pages polling would otherwise measure noise.
type cpuMeter struct {
	mu   sync.Mutex
	last map[string]cpuSample
}

type cpuSample struct {
	cpu time.Duration
	at  time.Time
	pct float64
}

const minInterval = 500 * time.Millisecond

func (m *cpuMeter) sample(now time.Time, cur map[string]time.Duration) map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]cpuSample, len(cur))
	pct := make(map[string]float64, len(cur))
	for k, c := range cur {
		prev, ok := m.last[k]
		switch {
		case !ok:
			next[k] = cpuSample{cpu: c, at: now}
		case now.Sub(prev.at) < minInterval:
			next[k] = prev
		default:
			// A process that exited takes its time with it: never below zero.
			p := float64(c-prev.cpu) / float64(now.Sub(prev.at)) * 100
			if p < 0 {
				p = 0
			}
			next[k] = cpuSample{cpu: c, at: now, pct: p}
		}
		pct[k] = next[k].pct
	}
	m.last = next // rows that stopped are forgotten
	return pct
}
