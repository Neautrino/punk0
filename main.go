// Package main implements punk0: a minimal agent orchestrator that runs coding
// agents in git worktrees behind a local browser UI.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var embedded embed.FS

// px0's policy minus its remote fonts and images. xterm.js injects <style> elements, hence 'unsafe-inline'.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'none';"

func main() {
	var (
		port = flag.Int("port", 7070, "port to listen on (0 picks a free one)")
		host = flag.String("host", "127.0.0.1", "address to bind")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "punk0 - agent orchestrator\n\nusage:\n  punk0 [flags] [dir]\n\ndir is added as a project; projects are kept in ~/.punk0/records.json.\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	rec, err := OpenRecords(filepath.Join(home, ".punk0"))
	if err != nil {
		log.Fatal(err)
	}
	if dir := flag.Arg(0); dir != "" {
		if _, err := rec.AddProject(dir); err != nil {
			log.Fatal(err)
		}
	}

	web, err := fs.Sub(embedded, "web")
	if err != nil {
		log.Fatal(err)
	}
	files := http.FileServerFS(web)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		files.ServeHTTP(w, r)
	})

	mux.HandleFunc("GET /api/projects", handleListProjects(rec))
	mux.HandleFunc("POST /api/projects", handleAddProject(rec))
	mux.HandleFunc("DELETE /api/projects/{id}", handleRemoveProject(rec))
	mux.HandleFunc("POST /api/projects/{id}/workspaces", handleAddWorkspace(rec))
	mux.HandleFunc("DELETE /api/workspaces/{id}", handleRemoveWorkspace(rec))
	mux.HandleFunc("GET /api/workspaces/{id}/tree", handleTree(rec))
	mux.HandleFunc("GET /api/workspaces/{id}/file", handleFile(rec))
	mux.HandleFunc("GET /api/workspaces/{id}/find", handleFind(rec))
	mux.HandleFunc("POST /api/workspaces/{id}/tabs", handleAddTab(rec))
	mux.HandleFunc("PATCH /api/tabs/{id}", handleRenameTab(rec))
	mux.HandleFunc("DELETE /api/tabs/{id}", handleRemoveTab(rec))
	mux.HandleFunc("GET /api/tabs/{id}/term", handleTerm(rec))

	ln, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		log.Fatal(err)
	}

	// Shells hold agents that may run for minutes; hang them up on exit
	// instead of leaving them orphaned under init.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		rec.Close()
		os.Exit(0)
	}()

	log.Printf("punk0 on http://%s", ln.Addr())
	log.Fatal(http.Serve(ln, guard(mux)))
}

func handleListProjects(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, rec.List())
	}
}

func handleAddProject(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Dir) == "" {
			failJSON(w, http.StatusBadRequest, "bad request body")
			return
		}
		p, err := rec.AddProject(expandHome(strings.TrimSpace(req.Dir)))
		if err != nil {
			failRecord(w, err)
			return
		}
		writeJSON(w, p)
	}
}

func handleRemoveProject(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rec.RemoveProject(r.PathValue("id")); err != nil {
			failRecord(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddWorkspace(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name     string
			Worktree bool
			Base     string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failJSON(w, http.StatusBadRequest, "bad request body")
			return
		}
		ws, err := rec.AddWorkspace(r.PathValue("id"), req.Name, req.Worktree, strings.TrimSpace(req.Base))
		if err != nil {
			failRecord(w, err)
			return
		}
		writeJSON(w, ws)
	}
}

// handleRemoveWorkspace answers 409 while a worktree has uncommitted
// changes; ?force=1 deletes them.
func handleRemoveWorkspace(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rec.RemoveWorkspace(r.PathValue("id"), r.URL.Query().Get("force") == "1"); err != nil {
			failRecord(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddTab(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Kind string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failJSON(w, http.StatusBadRequest, "bad request body")
			return
		}
		t, err := rec.AddTab(r.PathValue("id"), req.Kind)
		if err != nil {
			failRecord(w, err)
			return
		}
		writeJSON(w, t)
	}
}

func handleRenameTab(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failJSON(w, http.StatusBadRequest, "bad request body")
			return
		}
		if err := rec.RenameTab(r.PathValue("id"), req.Name); err != nil {
			failRecord(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveTab(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rec.RemoveTab(r.PathValue("id")); err != nil {
			failRecord(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// workspaceOf resolves the {id} path segment, answering 404 when it is unknown.
func workspaceOf(rec *Records, w http.ResponseWriter, r *http.Request) *Workspace {
	ws := rec.Workspace(r.PathValue("id"))
	if ws == nil {
		failJSON(w, http.StatusNotFound, "workspace not found")
	}
	return ws
}

// expandHome lets the add-project form take ~/path like a shell would.
func expandHome(dir string) string {
	if rest, ok := strings.CutPrefix(dir, "~"); ok && (rest == "" || rest[0] == '/') {
		if home, err := os.UserHomeDir(); err == nil {
			return home + rest
		}
	}
	return dir
}

func safePath(root, rel string) (string, string, bool) {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." {
		return root, "", true
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", "", false
	}
	abs := filepath.Join(root, clean)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", "", false
	}
	return abs, filepath.ToSlash(clean), true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func failJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// failRecord answers a Records error: 404 for unknown ids, 409 for a dirty
// worktree (the client may retry with force), 400 for the rest.
func failRecord(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, errNotFound):
		code = http.StatusNotFound
	case errors.Is(err, errDirty):
		code = http.StatusConflict
	}
	failJSON(w, code, err.Error())
}

func handleTree(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws := workspaceOf(rec, w, r)
		if ws == nil {
			return
		}
		ix := ws.Index()
		dir := strings.Trim(r.URL.Query().Get("dir"), "/")
		kids, ok := ix.Children(dir)
		if !ok && !ix.Ready() {
			for range 30 {
				time.Sleep(10 * time.Millisecond)
				if kids, ok = ix.Children(dir); ok {
					break
				}
				if ix.Ready() {
					kids, ok = ix.Children(dir)
					break
				}
			}
		}
		if !ok {
			failJSON(w, http.StatusNotFound, "not found: "+dir)
			return
		}
		writeJSON(w, map[string]any{"dir": dir, "children": kids})
	}
}

func handleFile(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws := workspaceOf(rec, w, r)
		if ws == nil {
			return
		}
		abs, rel, ok := safePath(ws.Dir, r.URL.Query().Get("path"))
		if !ok || rel == "" {
			failJSON(w, http.StatusBadRequest, "bad path")
			return
		}
		d, err := Open(abs, rel)
		if err != nil {
			failJSON(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		if count <= 0 {
			count = hlChunk
		}
		if start < 0 {
			start = 0
		}
		if start > d.Total {
			start = d.Total
		}
		lines, exact := d.Lines(start, start+count)
		writeJSON(w, map[string]any{
			"path":    rel,
			"lang":    d.Lang,
			"total":   d.Total,
			"maxCols": d.MaxCols,
			"start":   start,
			"lines":   lines,
			"exact":   exact,
		})
	}
}

// handleFind ranks the workspace's indexed files against ?q= for the open
// file picker. A first query waits briefly for the index to finish building.
func handleFind(rec *Records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws := workspaceOf(rec, w, r)
		if ws == nil {
			return
		}
		ix := ws.Index()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		ix.WaitReady(ctx) // an index still building answers from what it has
		writeJSON(w, map[string]any{"results": FuzzyFind(ix.Files(), r.URL.Query().Get("q"), 50)})
	}
}

// guard wraps every route: loopback Host on all requests (blocks DNS
// rebinding), same-origin on anything that is not GET/HEAD (blocks CSRF).
// Websocket upgrades are GETs; their Origin check lives in the upgrader.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			r.Header.Get("Origin") != "http://"+r.Host {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
