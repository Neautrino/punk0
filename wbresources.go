package main

// GET /api/resources: what punk0 costs right now, for the status bar's
// total and the resource panel it opens (web/src/respanel.js, on the agents
// page and in every editor). Every figure is a process tree from one
// process-table snapshot: a terminal is its shell and everything running in
// it (an agent and the tools it starts), an editor is its punk0 child alone,
// and its language servers are everything below that child. "self" is the
// parent plus anything below it that no row claims (git, a child still
// starting), so the rows add up to the total. Names come along, so a page
// without the workbench's store (an editor) can draw the panel too.

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"px0/internal/metrics"
)

// usage is one row: CPU in percent of one core, RSS and PSS in bytes, and for
// a punk0 process (the parent, an editor, a review) its goroutines, as px0's
// metrics showed them; 0 for anything else, or a child that did not answer.
// PSS is what the panel shows: RSS counts memory shared between processes
// (the agent binary every tab of one agent runs) once per process, so RSS
// rows add up to more than is used; PSS splits it between them. Where PSS
// cannot be read it equals RSS (metrics.Proc).
type usage struct {
	CPU        float64 `json:"cpu"`
	RSS        uint64  `json:"rss"`
	PSS        uint64  `json:"pss"`
	Goroutines int     `json:"goroutines,omitempty"`
}

// resTab is a running terminal: kind is the tab's (shell, claude, ...),
// status its agent status from the hooks.
type resTab struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	usage
}

// resWorkspace is a workspace with an editor or a terminal running; usage is
// their sum. Editor is its folder's child process alone, nil when the folder
// has no running child. LSP is everything below that child: its language
// servers, plus a git command now and then (short-lived, so a poll rarely
// catches one); nil with Editor.
type resWorkspace struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
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

// resources: usage is the total, Goroutines summed over every punk0 process.
type resources struct {
	usage
	Self       usage          `json:"self"`
	Workspaces []resWorkspace `json:"workspaces"`
	Reviews    []resItem      `json:"reviews"`
}

func (wb *workbench) handleResources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, wb.resources())
}

// childGoroutines asks each child's /api/metrics (which does not count as
// activity, so polling it keeps no editor from its idle stop) for its
// goroutines, all at once and briefly: a slow child shows none rather than
// holding up the panel.
func (wb *workbench) childGoroutines(urls map[string]string) map[string]int {
	out := make(map[string]int, len(urls))
	var mu sync.Mutex
	var wg sync.WaitGroup
	client := &http.Client{Timeout: time.Second}
	for id, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(u)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			var m struct {
				Goroutines int `json:"goroutines"`
			}
			if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&m) != nil {
				return
			}
			mu.Lock()
			out[id] = m.Goroutines
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func (wb *workbench) resources() resources {
	type child struct {
		id, title, addr string
		pid             int
	}
	var children []child
	wb.mu.Lock()
	for id, c := range wb.children {
		if c.state == "running" && c.cmd != nil && c.cmd.Process != nil {
			children = append(children, child{id, c.title, c.addr, c.cmd.Process.Pid})
		}
	}
	wb.mu.Unlock()
	selfGoroutines := runtime.NumGoroutine() // before the askers below add theirs
	urls := make(map[string]string, len(children))
	for _, c := range children {
		urls[c.id] = "http://" + c.addr + wb.childBase(c.id) + "api/metrics"
	}
	// The children answer while the process table is read.
	gorDone := make(chan map[string]int, 1)
	go func() { gorDone <- wb.childGoroutines(urls) }()
	running := wb.records.Running()

	out := resources{Workspaces: []resWorkspace{}, Reviews: []resItem{}}
	table := metrics.Processes()
	if table == nil { // no process table here: only the parent can be measured
		<-gorDone
		m := getProcessMetrics(nil)
		out.Self = usage{CPU: m.CPUUsage, RSS: m.RSSBytes, PSS: m.RSSBytes, Goroutines: selfGoroutines}
		out.usage = out.Self
		return out
	}

	// Measure every tree first, then turn CPU times into percentages in one
	// pass, so all rows share one interval.
	cpuTimes := map[string]time.Duration{}
	rss := map[string]uint64{}
	pss := map[string]uint64{}
	measure := func(key string, pid int) {
		p := table.Tree(pid)
		cpuTimes[key], rss[key], pss[key] = p.CPU, p.RSS, p.PSS
	}
	// An editor's tree splits in two rows: its own process, and everything
	// below it (its language servers). Together they are the tree, so the
	// claimed sum below stays a sum of disjoint trees.
	measureEditor := func(id string, pid int) {
		tree, own := table.Tree(pid), table.One(pid)
		cpuTimes["editor:"+id], rss["editor:"+id], pss["editor:"+id] = own.CPU, own.RSS, own.PSS
		cpuTimes["lsp:"+id], rss["lsp:"+id], pss["lsp:"+id] = tree.CPU-own.CPU, tree.RSS-own.RSS, tree.PSS-own.PSS
	}
	all := table.Tree(os.Getpid())
	cpuTimes["total"], rss["total"], pss["total"] = all.CPU, all.RSS, all.PSS
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
	var claimedRSS, claimedPSS uint64
	for k, v := range cpuTimes {
		if k != "total" {
			claimedCPU += v
			claimedRSS += rss[k]
			claimedPSS += pss[k]
		}
	}
	// The claimed trees all sit below the parent, so they fit in the total.
	cpuTimes["self"], rss["self"], pss["self"] = 0, 0, 0
	if all.CPU > claimedCPU {
		cpuTimes["self"] = all.CPU - claimedCPU
	}
	if all.RSS > claimedRSS {
		rss["self"] = all.RSS - claimedRSS
	}
	if all.PSS > claimedPSS {
		pss["self"] = all.PSS - claimedPSS
	}

	pct := wb.cpu.sample(time.Now(), cpuTimes)
	row := func(key string) usage { return usage{CPU: pct[key], RSS: rss[key], PSS: pss[key]} }
	gor := <-gorDone

	out.usage, out.Self = row("total"), row("self")
	out.Self.Goroutines = selfGoroutines
	out.Goroutines = selfGoroutines
	for _, n := range gor {
		out.Goroutines += n
	}
	for _, s := range shown {
		ws := resWorkspace{ID: s.ws.id, Name: s.ws.name, Tabs: []resTab{}}
		if s.editor != "" {
			e, l := row("editor:"+s.editor), row("lsp:"+s.editor)
			e.Goroutines = gor[s.editor]
			ws.Editor, ws.LSP = &e, &l
			ws.CPU, ws.RSS, ws.PSS = e.CPU+l.CPU, e.RSS+l.RSS, e.PSS+l.PSS
		}
		for _, t := range s.ws.tabs {
			u := row("tab:" + t.id)
			ws.Tabs = append(ws.Tabs, resTab{ID: t.id, Name: t.name, Kind: t.kind, Status: t.status, usage: u})
			ws.CPU += u.CPU
			ws.RSS += u.RSS
			ws.PSS += u.PSS
		}
		out.Workspaces = append(out.Workspaces, ws)
	}
	for _, c := range children {
		if _, ok := editorOf[c.id]; ok {
			u := row("item:" + c.id)
			u.Goroutines = gor[c.id]
			out.Reviews = append(out.Reviews, resItem{c.id, c.title, u})
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
