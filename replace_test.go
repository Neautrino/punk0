package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFileReplaceEndpoint(t *testing.T) {
	s, root := newTestServer(t)

	filePath := filepath.Join(root, "replace_sample.txt")
	originalContent := "line 1\nline 2\nline 3\nline 4\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Successful replace
	payload := replaceRequest{
		Path:        "replace_sample.txt",
		L1:          2,
		L2:          3,
		Original:    "line 2\nline 3",
		Replacement: "line 2 modified\nline 3 modified",
	}
	body, _ := json.Marshal(payload)
	req := localJSONPost(s.routePath("/api/file/replace"), body)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	content, _ := os.ReadFile(filePath)
	expected := "line 1\nline 2 modified\nline 3 modified\nline 4\n"
	if string(content) != expected {
		t.Fatalf("unexpected content:\n%s\nexpected:\n%s", string(content), expected)
	}

	// 2. Conflict replace (original does not match)
	conflictPayload := replaceRequest{
		Path:        "replace_sample.txt",
		L1:          2,
		L2:          3,
		Original:    "old line 2\nold line 3",
		Replacement: "new",
	}
	body, _ = json.Marshal(conflictPayload)
	req = localJSONPost(s.routePath("/api/file/replace"), body)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
	}
}
