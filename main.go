// Package main implements punk0: a minimal agent orchestrator that runs coding
// agents in git worktrees behind a local browser UI.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
)

//go:embed web
var embedded embed.FS

// px0's policy minus its remote fonts and images. xterm.js injects <style> elements, hence 'unsafe-inline'.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'none';"

func main() {
	var (
		port = flag.Int("port", 7070, "port to listen on (0 picks a free one)")
		host = flag.String("host", "127.0.0.1", "address to bind")
		dir  = flag.String("dir", ".", "worktree the shell and agents run in")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "punk0 - agent orchestrator\n\nusage:\n  punk0 [flags]\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Check here: with a PTY, a missing dir surfaces only as a misleading
	// "fork/exec <shell>: no such file or directory" when the tab connects.
	if fi, err := os.Stat(*dir); err != nil {
		log.Fatal(err)
	} else if !fi.IsDir() {
		log.Fatalf("%s is not a directory", *dir)
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
	mux.Handle("GET /term", &termHandler{dir: *dir})

	ln, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("punk0 on http://%s", ln.Addr())
	log.Fatal(http.Serve(ln, guard(mux)))
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
