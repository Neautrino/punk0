package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Quick Edit's Accept: lines are swapped only while they still read what the
// preview was made from, in the file's own line endings.
func TestFileReplace(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		req      replaceRequest
		code     int
		want     string // file afterwards
	}{
		{"replaces lines", "a\nb\nc\nd\n",
			replaceRequest{L1: 2, L2: 3, Original: "b\nc", Replacement: "B\nC\nC2"}, 200,
			"a\nB\nC\nC2\nd\n"},
		{"refuses lines changed since", "a\nb\nc\n",
			replaceRequest{L1: 2, L2: 2, Original: "old b", Replacement: "x"}, 409,
			"a\nb\nc\n"},
		{"keeps CRLF endings", "a\r\nb\r\nc\r\n",
			replaceRequest{L1: 1, L2: 2, Original: "a\nb", Replacement: "A\nB"}, 200,
			"A\r\nB\r\nc\r\n"},
		{"last line without newline stays so", "a\r\nb",
			replaceRequest{L1: 2, L2: 2, Original: "b", Replacement: "B1\nB2"}, 200,
			"a\r\nB1\r\nB2"},
		{"empty replacement deletes", "a\nb\nc\n",
			replaceRequest{L1: 2, L2: 2, Original: "b", Replacement: ""}, 200,
			"a\nc\n"},
		{"range past the end", "a\nb\n",
			replaceRequest{L1: 2, L2: 3, Original: "b\n", Replacement: "x"}, 400,
			"a\nb\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, root := newTestServer(t)
			p := filepath.Join(root, "f.txt")
			if err := os.WriteFile(p, []byte(c.file), 0o644); err != nil {
				t.Fatal(err)
			}
			c.req.Path = "f.txt"
			body, _ := json.Marshal(c.req)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, localJSONPost(s.routePath("/api/file/replace"), body))
			if rec.Code != c.code {
				t.Fatalf("code %d, want %d: %s", rec.Code, c.code, rec.Body.String())
			}
			got, _ := os.ReadFile(p)
			if string(got) != c.want {
				t.Fatalf("file %q, want %q", got, c.want)
			}
		})
	}
}
