package main

import (
	"strings"
	"testing"
)

func entries(paths ...string) []FileEntry {
	out := make([]FileEntry, len(paths))
	for i, p := range paths {
		name := p[strings.LastIndex(p, "/")+1:]
		out[i] = FileEntry{Path: p, Name: name, lower: strings.ToLower(p), nameStart: len(p) - len(name)}
	}
	return out
}

func TestFuzzyRanking(t *testing.T) {
	files := entries(
		"internal/server/http_server.go",
		"cmd/px0/main.go",
		"web/app.js",
		"pkg/util/strings.go",
		"vendor/github.com/x/http/server.go",
		"httpserver.go",
	)

	got := FuzzyFind(files, "httpserver", 10)
	if len(got) == 0 || got[0].Path != "httpserver.go" {
		t.Fatalf("best match for httpserver = %v, want httpserver.go first", got)
	}
	for _, r := range got {
		if len(r.Pos) != len("httpserver") {
			t.Fatalf("%q: %d highlight positions, want %d", r.Path, len(r.Pos), len("httpserver"))
		}
		for k, p := range r.Pos {
			if p < 0 || p >= len(r.Path) || strings.ToLower(r.Path[p:p+1]) != "httpserver"[k:k+1] {
				t.Errorf("%q: position %d does not match query byte %q", r.Path, p, "httpserver"[k])
			}
		}
	}
	if got := FuzzyFind(files, "zzqq", 10); len(got) != 0 {
		t.Errorf("zzqq matched %v", got)
	}
	if got := FuzzyFind(files, "appjs", 10); len(got) == 0 || got[0].Path != "web/app.js" {
		t.Errorf("appjs = %v, want web/app.js first", got)
	}
}

func TestFuzzyExactCaseRanksHigher(t *testing.T) {
	got := FuzzyFind(entries("src/httpserver.go", "src/HTTPServer.go"), "HTTPServer", 10)
	if len(got) != 2 || got[0].Path != "src/HTTPServer.go" {
		t.Fatalf("HTTPServer = %v, want src/HTTPServer.go first", got)
	}
}
