// Package main implements punk0: a minimal agent orchestrator that runs coding
// agents in git worktrees behind a local browser UI.
package main

import (
	"embed"
	"encoding/json"
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
		dir  = flag.String("dir", ".", "primary worktree; satellites are added to its repository")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "punk0 - agent orchestrator\n\nusage:\n  punk0 [flags]\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	workdir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if fi, err := os.Stat(workdir); err != nil {
		log.Fatal(err)
	} else if !fi.IsDir() {
		log.Fatalf("%s is not a directory", workdir)
	}

	reg := NewRegistry(workdir)

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

	mux.HandleFunc("GET /api/satellites", handleListSatellites(reg))
	mux.HandleFunc("POST /api/satellites", handleCreateSatellite(reg))
	mux.HandleFunc("DELETE /api/satellites/{id}", handleDeleteSatellite(reg))
	mux.HandleFunc("GET /api/satellites/{id}/tree", handleTree(reg))
	mux.HandleFunc("GET /api/satellites/{id}/file", handleFile(reg))
	mux.HandleFunc("GET /api/satellites/{id}/term", handleTerm(reg))

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
		reg.Close()
		os.Exit(0)
	}()

	log.Printf("punk0 on http://%s", ln.Addr())
	log.Fatal(http.Serve(ln, guard(mux)))
}

func handleListSatellites(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, reg.List())
	}
}

func handleCreateSatellite(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, Branch string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failJSON(w, http.StatusBadRequest, "bad request body")
			return
		}
		sat, err := reg.Create(strings.TrimSpace(req.Name), strings.TrimSpace(req.Branch))
		if err != nil {
			failJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, sat)
	}
}

func handleDeleteSatellite(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sat := satelliteOf(reg, w, r)
		if sat == nil {
			return
		}
		if err := reg.Remove(sat.ID); err != nil {
			failJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
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

func handleTree(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sat := satelliteOf(reg, w, r)
		if sat == nil {
			return
		}
		ix := sat.Index()
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

func handleFile(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sat := satelliteOf(reg, w, r)
		if sat == nil {
			return
		}
		abs, rel, ok := safePath(sat.Dir, r.URL.Query().Get("path"))
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
