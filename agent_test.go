package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/px0-ai/harness"
)

// writeHarness creates an executable stand-in for a coding harness, outside the
// workspace so that it does not show up as a change the run made.
func writeHarness(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "harness.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolateSettings points the settings file at a temp dir, so a test never reads
// or overwrites the choice the developer running it has made.
func isolateSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// agentPost speaks the way the browser does: POST, with an Origin that matches
// a Host localPost will accept.
func agentPost(t *testing.T, s *Server, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func agentPostJSON(t *testing.T, s *Server, url string, body any) (int, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// agentEdit dispatches one anchored edit through /api/agent/batch.
func agentEdit(t *testing.T, s *Server, path string, l1, l2 int, instruction string) (int, map[string]any) {
	t.Helper()
	return agentPostJSON(t, s, "/api/agent/batch", map[string]any{"edits": []map[string]any{
		{"path": path, "l1": l1, "l2": l2, "instruction": instruction},
	}})
}

func agentServer(t *testing.T, root, harness string) *Server {
	t.Helper()
	isolateSettings(t)
	m, err := newAgentManager(root, harness+" {prompt}", nil)
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	s.SetAgent(m)
	t.Cleanup(func() {
		if s.gitWatcher != nil {
			s.gitWatcher.Stop()
		}
	})
	return s
}

// waitIdle waits for the most recently started job to finish. Tests that keep
// several jobs in flight at once poll a specific id instead.
func waitIdle(t *testing.T, s *Server) *agentJob {
	t.Helper()
	return waitIdleID(t, s, 0)
}

func waitIdleID(t *testing.T, s *Server, id int64) *agentJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if j := s.agent.Job(id); j != nil && !j.Running {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("harness did not finish")
	return nil
}

func TestAgentSpecResolution(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()

	if _, err := newAgentManager(root, "echo hello", nil); err == nil {
		t.Fatal("a template without {prompt} should be refused")
	}
	if _, err := newAgentManager(root, "px0-not-a-real-binary {prompt}", nil); err == nil {
		t.Fatal("a missing binary should be refused at startup, not on first use")
	}

	m, err := newAgentManager(root, "echo {prompt}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name() != "echo" {
		t.Fatalf("name = %q, want echo", m.Name())
	}
	if !m.Pinned() {
		t.Fatal("-agent should pin the harness")
	}
	if err := m.Select("echo {prompt}"); err == nil {
		t.Fatal("a pinned harness must not be changeable from the UI")
	}

	// No flag and no saved choice: editing is available, nothing is selected.
	idle, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if idle.Name() != "" || idle.Pinned() {
		t.Fatalf("fresh manager = %q pinned=%v, want unselected and unpinned", idle.Name(), idle.Pinned())
	}
}

func TestAgentDetectListsKnownHarnesses(t *testing.T) {
	isolateSettings(t)
	m, err := newAgentManager(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Detect()
	presets := harness.Presets()
	if len(got) != len(presets) {
		t.Fatalf("detected %d rows, want one per preset (%d)", len(got), len(presets))
	}
	for i, h := range got {
		if h.Name != presets[i].Name {
			t.Fatalf("row %d = %q, want %q", i, h.Name, presets[i].Name)
		}
		if !strings.Contains(h.Cmd, "{prompt}") {
			t.Fatalf("%s cmd should show the template, got %q", h.Name, h.Cmd)
		}
		if h.Installed && h.Path == "" {
			t.Fatalf("%s reported installed with no path", h.Name)
		}
	}
}

func TestAgentDetectLazyModels(t *testing.T) {
	isolateSettings(t)
	m, err := newAgentManager(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Detect()
	for _, h := range got {
		if len(h.Models) > 0 {
			t.Fatalf("harness %q had models eagerly discovered when unselected", h.Name)
		}
	}
}

func TestAgentSelectPersistsOutsideWorkspace(t *testing.T) {
	cfg := isolateSettings(t)
	root := t.TempDir()

	m, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Select("px0-not-a-real-binary"); err == nil {
		t.Fatal("selecting something that is not installed should fail")
	}

	// echo stands in for a harness binary that exists on every machine.
	if err := m.Select("echo {prompt}"); err != nil {
		t.Fatal(err)
	}
	if m.Name() != "echo" {
		t.Fatalf("selected = %q, want echo", m.Name())
	}

	if _, err := os.Stat(filepath.Join(cfg, "punk0", "settings.json")); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("settings must never be written into the workspace")
	}

	// A later run restores the choice, template and all.
	restored, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name() != "echo" {
		t.Fatalf("restored = %q, want echo", restored.Name())
	}

	if err := restored.Select(""); err != nil {
		t.Fatal(err)
	}
	if restored.Name() != "" {
		t.Fatalf("cleared = %q, want empty", restored.Name())
	}
	if again, _ := newAgentManager(root, "", nil); again.Name() != "" {
		t.Fatalf("clearing did not persist, got %q", again.Name())
	}
}

func TestAgentHarnessEndpointsRequireAvailability(t *testing.T) {
	s, _ := newTestServer(t) // no SetAgent: editing unavailable

	if code, _ := get(t, s, "/api/agent/job"); code != http.StatusNotFound {
		t.Fatalf("job = %d, want 404", code)
	}
	if code, _ := get(t, s, "/api/agent/harnesses"); code != http.StatusNotFound {
		t.Fatalf("harnesses = %d, want 404", code)
	}
	if code, _ := agentEdit(t, s, "main.go", 1, 1, "hi"); code != http.StatusNotFound {
		t.Fatalf("edit = %d, want 404", code)
	}

	_, meta := get(t, s, "/api/meta")
	if meta["agent"] != "" {
		t.Fatalf("meta agent = %v, want empty", meta["agent"])
	}
	if got, ok := meta["agents"].([]any); !ok || len(got) != 0 {
		t.Fatalf("meta agents = %v, want an empty list", meta["agents"])
	}
}

func TestAgentEditRefusedUntilHarnessChosen(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := newAgentManager(root, "", nil) // available, nothing selected
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	s.localOnly = true // a command template is only accepted on loopback
	s.SetAgent(m)

	code, body := agentEdit(t, s, "a.go", 1, 1, "hi")
	if code != 400 {
		t.Fatalf("edit with nothing selected = %d, want 400", code)
	}
	if !strings.Contains(body["error"].(string), "no coding harness") {
		t.Fatalf("error = %q", body["error"])
	}

	// Picking one over HTTP is enough to make editing work.
	if code, _ = agentPost(t, s, "/api/agent/select?name="+"echo+%7Bprompt%7D"); code != 200 {
		t.Fatalf("select = %d, want 200", code)
	}
	if code, _ = agentEdit(t, s, "a.go", 1, 1, "hi"); code != 200 {
		t.Fatalf("edit after select = %d, want 200", code)
	}
	waitIdle(t, s)
}

func TestAgentEditRunsHarnessAndReportsChange(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	out := t.TempDir()
	// keep.go is committed and clean, so no force is needed.
	s := agentServer(t, root, writeHarness(t,
		"printf 'touched\\n' >> keep.go\nprintf '%s' \"$1\" > "+filepath.Join(out, "prompt.txt")+"\n"))

	code, _ := agentEdit(t, s, "keep.go", 1, 1, "add a line")
	if code != 200 {
		t.Fatalf("edit = %d, want 200", code)
	}

	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("harness failed: %s (log: %s)", job.Error, job.Log)
	}

	body, err := os.ReadFile(filepath.Join(root, "keep.go"))
	if err != nil || !strings.Contains(string(body), "touched") {
		t.Fatalf("harness did not edit the file: %q %v", body, err)
	}
	if len(job.Changed) != 1 || job.Changed[0] != "keep.go" {
		t.Fatalf("changed = %v, want [keep.go]", job.Changed)
	}
	if !job.Tracked {
		t.Fatal("tracked should be true inside a repository")
	}

	// The prompt must carry the anchor and the instruction the user wrote.
	prompt, err := os.ReadFile(filepath.Join(out, "prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@keep.go 1", "add a line"} {
		if !strings.Contains(string(prompt), want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// Outside a repository px0 cannot name what a harness touched. The job must say
// so, because an empty change list would otherwise read as "nothing happened"
// and the client would skip the reload after a real edit.
func TestAgentOutsideGitReportsUnknownChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> a.go\n"))

	// With no git there is also no uncommitted-work guard to satisfy.
	if code, _ := agentEdit(t, s, "a.go", 1, 1, "hi"); code != 200 {
		t.Fatalf("edit = %d, want 200", code)
	}
	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("run failed: %s (log: %s)", job.Error, job.Log)
	}
	if job.Tracked {
		t.Fatal("tracked should be false outside a repository")
	}
	if len(job.Changed) != 0 {
		t.Fatalf("changed = %v, want empty outside a repository", job.Changed)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if !strings.Contains(string(body), "touched") {
		t.Fatalf("harness did not edit the file: %q", body)
	}
}

func TestAgentRefusesSecondEditWhileRunning(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "sleep 2\n"))

	if code, _ := agentEdit(t, s, "keep.go", 1, 1, "one"); code != 200 {
		t.Fatalf("first edit = %d, want 200", code)
	}
	code, body := agentEdit(t, s, "keep.go", 1, 1, "two")
	if code != http.StatusConflict {
		t.Fatalf("second edit = %d, want 409", code)
	}
	if !strings.Contains(body["error"].(string), "already running") {
		t.Fatalf("error = %q", body["error"])
	}

	s.agent.Cancel()
	waitIdle(t, s)
}

func TestAgentAllowsEditOverUncommittedFileWithoutForce(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	// sub/mod.go is modified but not committed by gitRepo.
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> keep.go\n"))

	code, _ := agentEdit(t, s, "sub/mod.go", 1, 1, "hi")
	if code != http.StatusOK {
		t.Fatalf("edit over uncommitted work = %d, want 200", code)
	}
	if job := waitIdle(t, s); job.Error != "" {
		t.Fatalf("run failed: %s", job.Error)
	}
}

func TestAgentModelSelectionAndDefaults(t *testing.T) {
	isolateSettings(t)
	m, err := newAgentManager(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := m.Detect()
	if len(rows) == 0 {
		t.Fatal("expected detected rows")
	}
	for _, h := range rows {
		if !h.Installed {
			continue
		}
		if err := m.Select(h.Name, "custom-model"); err != nil {
			t.Fatalf("select %s with model: %v", h.Name, err)
		}
		if m.Model() != "custom-model" {
			t.Fatalf("model = %q, want custom-model", m.Model())
		}
	}
}

func TestPresetArgvOrder(t *testing.T) {
	for _, p := range harness.Presets() {
		n := len(p.Args)
		if n < 2 {
			t.Fatalf("preset %s args too short: %v", p.Name, p.Args)
		}
		if p.Args[n-1] != "{prompt}" {
			t.Fatalf("preset %s args %v: want {prompt} at the very end", p.Name, p.Args)
		}

		// When resolved, {prompt} must remain at the very end
		_, resolved, _, err := resolveAgentSpec(p.Name, "")
		if err != nil {
			continue // tool may not be installed in test env
		}
		rn := len(resolved)
		if rn < 2 || resolved[rn-1] != "{prompt}" {
			t.Fatalf("resolved %s args %v: want {prompt} at the very end", p.Name, resolved)
		}
	}
}

func TestClaudeModelDiscovery(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho 'Current model: Sonnet 5'\necho 'Usage: /model <name>. Available: sonnet, opus, haiku, fable, best, sonnet[1m], opus[1m], fable[1m], opusplan, default, or a full model ID.'\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	models, err := harness.DiscoverModels(context.Background(), "claude")
	if err != nil {
		t.Fatalf("DiscoverModels error: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("expected discovered models for claude, got none")
	}
	foundFable := false
	for _, m := range models {
		if m == "fable" {
			foundFable = true
			break
		}
	}
	if !foundFable {
		t.Fatalf("expected 'fable' in discovered models: %v", models)
	}
}

// Editing in the diff view means editing a file that is already modified. Its
// git status reads M before and after, so the change has to be seen some other way.
func TestAgentReportsEditToAlreadyModifiedFile(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> sub/mod.go\n"))

	if code, _ := agentEdit(t, s, "sub/mod.go", 1, 1, "hi"); code != 200 {
		t.Fatalf("edit = %d, want 200", code)
	}
	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("harness failed: %s", job.Error)
	}
	if len(job.Changed) != 1 || job.Changed[0] != "sub/mod.go" {
		t.Fatalf("changed = %v, want [sub/mod.go]", job.Changed)
	}
}

// Two edits on disjoint line ranges of the same file run at the same time;
// only an overlapping range is refused.
func TestAgentAllowsNonOverlappingEditsInParallel(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "sleep 0.15\n"))

	code, first := agentEdit(t, s, "keep.go", 1, 1, "one")
	if code != 200 {
		t.Fatalf("first edit = %d, want 200", code)
	}
	code, _ = agentEdit(t, s, "keep.go", 2, 2, "two")
	if code != 200 {
		t.Fatalf("disjoint-range edit = %d, want 200 (should run in parallel)", code)
	}
	code, _ = agentEdit(t, s, "keep.go", 1, 2, "three")
	if code != http.StatusConflict {
		t.Fatalf("edit overlapping both = %d, want 409", code)
	}

	firstID := int64(first["id"].(float64))
	if job := waitIdleID(t, s, firstID); job.Error != "" {
		t.Fatalf("first edit failed: %s", job.Error)
	}
}

func TestAgentMutationsRejectCrossOriginPost(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> keep.go\n"))

	for _, path := range []string{
		"/api/agent/batch",
		"/api/agent/select?name=claude",
		"/api/agent/cancel",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Host = "127.0.0.1:7777"
		req.Header.Set("Origin", "http://evil.example.com")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s = %d, want 403", path, rec.Code)
		}

		if code, _ := get(t, s, path); code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s = %d, want 405", path, code)
		}
	}
}

func TestAgentCancelJob(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	// Harness that sleeps to simulate a long-running edit
	s := agentServer(t, root, writeHarness(t, "sleep 5\n"))

	code, first := agentEdit(t, s, "keep.go", 1, 1, "first")
	if code != http.StatusOK {
		t.Fatalf("edit first = %d, want 200", code)
	}
	id := int64(first["id"].(float64))

	// Cancel by id
	cancelCode, cancelBody := agentPost(t, s, fmt.Sprintf("/api/agent/cancel?id=%d", id))
	if cancelCode != http.StatusOK {
		t.Fatalf("cancel = %d, want 200", cancelCode)
	}
	if cancelled, _ := cancelBody["cancelled"].(bool); !cancelled {
		t.Fatalf("cancelled = false, want true")
	}

	// Job should be stopped
	time.Sleep(100 * time.Millisecond)
	code, job := get(t, s, fmt.Sprintf("/api/agent/job?id=%d", id))
	if code != http.StatusOK {
		t.Fatalf("job = %d, want 200", code)
	}
	if running, _ := job["running"].(bool); running {
		t.Fatalf("job still running after cancel")
	}
}

func TestReadLineRange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		l1, l2 int
		want   string
	}{
		{2, 3, "two\nthree"},
		{1, 1, "one"},
		{3, 99, "three\nfour\n"}, // clamped to the end, trailing blank line included
		{0, 1, "one"},            // l1 below 1 is clamped
	} {
		got, err := readLineRange(p, tc.l1, tc.l2)
		if err != nil {
			t.Fatalf("%d-%d: %v", tc.l1, tc.l2, err)
		}
		if got != tc.want {
			t.Fatalf("%d-%d = %q, want %q", tc.l1, tc.l2, got, tc.want)
		}
	}
	if _, err := readLineRange(p, 50, 60); err == nil {
		t.Fatal("a range past the end should fail")
	}
}

func TestChangedSinceReportsBothDirections(t *testing.T) {
	before := map[string]string{"stays.go": "M", "reverted.go": "M"}
	after := map[string]string{"stays.go": "M", "new.go": "U"}

	got := map[string]bool{}
	for _, p := range changedSinceMaps(before, after) {
		got[p] = true
	}
	if got["stays.go"] {
		t.Fatal("an unchanged status should not be reported")
	}
	if !got["new.go"] {
		t.Fatal("a newly dirty file should be reported")
	}
	if !got["reverted.go"] {
		t.Fatal("a file restored to its committed state should be reported")
	}
}

// The job summary and API response list changed files, so their order must not
// follow Go's randomised map iteration. Repeated to catch a lucky ordering.
func TestChangedSinceIsSorted(t *testing.T) {
	before := map[string]string{"b.go": "M", "d.go": "M"}
	after := map[string]string{"c.go": "U", "a.go": "M", "d.go": "M"}
	for i := 0; i < 20; i++ {
		if got := strings.Join(changedSinceMaps(before, after), ","); got != "a.go,b.go,c.go" {
			t.Fatalf("changed = %q, want a.go,b.go,c.go", got)
		}
	}
}

func TestLineRef(t *testing.T) {
	if lineRef(4, 4) != "4" {
		t.Fatalf("single line ref = %q", lineRef(4, 4))
	}
	if lineRef(4, 9) != "4-9" {
		t.Fatalf("range ref = %q", lineRef(4, 9))
	}
}

func TestAllPresetArgvFormatting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	for _, p := range harness.Presets() {
		binPath := filepath.Join(dir, p.Args[0])
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	for _, p := range harness.Presets() {
		name, resolved, model, err := resolveAgentSpec(p.Name, "test-model")
		if err != nil {
			t.Fatalf("resolveAgentSpec(%q) error: %v", p.Name, err)
		}
		if name != p.Name {
			t.Errorf("name = %q, want %q", name, p.Name)
		}
		if model != "test-model" {
			t.Errorf("model = %q, want %q", model, "test-model")
		}
		if resolved[len(resolved)-1] != "{prompt}" {
			t.Errorf("%s final arg = %q, want {prompt}", p.Name, resolved[len(resolved)-1])
		}
		// Ensure model flag was inserted properly
		hasModel := false
		for i, a := range resolved {
			if a == p.ModelFlag && i+1 < len(resolved) && resolved[i+1] == "test-model" {
				hasModel = true
				break
			}
		}
		if !hasModel {
			t.Errorf("%s resolved args %v missing model flag %q %q", p.Name, resolved, p.ModelFlag, "test-model")
		}
	}
}

func TestAgentBatchEditRejectsIntraBatchOverlap(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\nvar X = 1\nvar Y = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := newAgentManager(root, "echo {prompt}", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Overlapping ranges on the same file: lines 1-2 and lines 2-3
	items := []agentBatchItem{
		{Abs: filepath.Join(root, "a.go"), Path: "a.go", L1: 1, L2: 2, Instruction: "edit 1"},
		{Abs: filepath.Join(root, "a.go"), Path: "a.go", L1: 2, L2: 3, Instruction: "edit 2"},
	}
	if _, err := m.StartEdit(items); err == nil {
		t.Fatal("expected overlapping batch edits on same file to be rejected, got nil error")
	}

	// Disjoint ranges on the same file: lines 1-1 and lines 3-3 (should succeed)
	validItems := []agentBatchItem{
		{Abs: filepath.Join(root, "a.go"), Path: "a.go", L1: 1, L2: 1, Instruction: "edit 1"},
		{Abs: filepath.Join(root, "a.go"), Path: "a.go", L1: 3, L2: 3, Instruction: "edit 2"},
	}
	job, err := m.StartEdit(validItems)
	if err != nil {
		t.Fatalf("expected disjoint batch edits to succeed, got %v", err)
	}
	if job.Path != "2 edits" {
		t.Errorf("job.Path = %q, want %q", job.Path, "2 edits")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j := m.Job(job.ID); j != nil && !j.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentBatchEditRunsHarness(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)

	s := agentServer(t, root, writeHarness(t,
		`echo "// edited 1" >> keep.go
echo "// edited 2" >> sub/mod.go
`))

	payload := map[string]any{
		"edits": []map[string]any{
			{"path": "keep.go", "l1": 1, "l2": 1, "instruction": "edit keep"},
			{"path": "sub/mod.go", "l1": 1, "l2": 1, "instruction": "edit mod"},
		},
	}
	code, body := agentPostJSON(t, s, "/api/agent/batch", payload)
	if code != 200 {
		t.Fatalf("batch edit = %d, want 200 (error: %v)", code, body["error"])
	}
	id := int64(body["id"].(float64))
	j := waitIdleID(t, s, id)
	if j.Error != "" {
		t.Fatalf("batch job failed: %s", j.Error)
	}
	if len(j.Changed) != 2 {
		t.Fatalf("changed = %v, want 2 files", j.Changed)
	}
}

func TestAgentAllowsIndividualAndBatchEditsConcurrently(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)

	s := agentServer(t, root, writeHarness(t,
		`sleep 0.2
echo "// edited" >> keep.go
`))

	// Start individual edit on keep.go line 1
	code1, body1 := agentEdit(t, s, "keep.go", 1, 1, "one")
	if code1 != 200 {
		t.Fatalf("edit 1 = %d, want 200 (error: %v)", code1, body1["error"])
	}
	id1 := int64(body1["id"].(float64))

	// Attempting an overlapping batch edit on keep.go:1-2 must be rejected
	overlapPayload := map[string]any{
		"edits": []map[string]any{
			{"path": "keep.go", "l1": 1, "l2": 2, "instruction": "overlap"},
		},
	}
	overlapCode, _ := agentPostJSON(t, s, "/api/agent/batch", overlapPayload)
	if overlapCode != 409 {
		t.Fatalf("overlapping batch edit = %d, want 409", overlapCode)
	}

	// Non-overlapping batch edit on keep.go:2-2 and sub/mod.go:1-1 must be accepted concurrently
	batchPayload := map[string]any{
		"edits": []map[string]any{
			{"path": "keep.go", "l1": 2, "l2": 2, "instruction": "non-overlap keep"},
			{"path": "sub/mod.go", "l1": 1, "l2": 1, "instruction": "non-overlap mod"},
		},
	}
	code2, body2 := agentPostJSON(t, s, "/api/agent/batch", batchPayload)
	if code2 != 200 {
		t.Fatalf("batch edit 2 = %d, want 200 (error: %v)", code2, body2["error"])
	}
	id2 := int64(body2["id"].(float64))

	j1 := waitIdleID(t, s, id1)
	j2 := waitIdleID(t, s, id2)
	if j1.Error != "" {
		t.Fatalf("job 1 failed: %s", j1.Error)
	}
	if j2.Error != "" {
		t.Fatalf("job 2 failed: %s", j2.Error)
	}
}

// /api/agent/batch takes only {"edits": [...]}, the shape pr.js sends.
func TestAgentBatchEditPayload(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "exit 0\n"))

	for name, payload := range map[string]any{
		"empty":    map[string]any{"edits": []any{}},
		"bare":     []map[string]any{{"path": "keep.go", "l1": 1, "l2": 1, "instruction": "arr"}},
		"no edits": map[string]any{"path": "keep.go", "l1": 1, "l2": 1, "instruction": "single"},
	} {
		code, body := agentPostJSON(t, s, "/api/agent/batch", payload)
		if code != 400 || body["error"] != "invalid or empty batch edits payload" {
			t.Fatalf("%s payload = %d %v, want 400", name, code, body["error"])
		}
	}
	if code, body := agentPostJSON(t, s, "/api/agent/batch", map[string]any{"edits": []map[string]any{
		{"path": "keep.go", "l1": 1, "l2": 1, "instruction": "  "},
	}}); code != 400 || body["error"] != "instruction is empty" {
		t.Fatalf("blank instruction = %d %v, want 400", code, body["error"])
	}
}

func TestCommitMessagePrompt(t *testing.T) {
	files := []string{"web/src/gitpanel.js", "server.go"}
	stat := " web/src/gitpanel.js | 15 +++\n server.go           | 40 ++-\n 2 files changed, 45 insertions(+), 10 deletions(-)"
	diff := "diff --git a/server.go b/server.go\n--- a/server.go\n+++ b/server.go\n@@ -1 +1 @@\n-old\n+new"
	instruction := "Follow conventional commits format"

	p := commitMessagePrompt(files, stat, diff, instruction)
	if !strings.Contains(p, "Changed files (2):") {
		t.Errorf("prompt missing changed files header:\n%s", p)
	}
	if !strings.Contains(p, "- web/src/gitpanel.js") || !strings.Contains(p, "- server.go") {
		t.Errorf("prompt missing changed file paths:\n%s", p)
	}
	if !strings.Contains(p, "Summary of changes (diffstat):") || !strings.Contains(p, stat) {
		t.Errorf("prompt missing diffstat summary:\n%s", p)
	}
	if !strings.Contains(p, "Staged diff:") || !strings.Contains(p, diff) {
		t.Errorf("prompt missing staged diff:\n%s", p)
	}
	if !strings.Contains(p, "Additional instructions from the user: "+instruction) {
		t.Errorf("prompt missing user instruction:\n%s", p)
	}

	// Test truncation when there are > 100 files
	manyFiles := make([]string, 125)
	for i := range manyFiles {
		manyFiles[i] = fmt.Sprintf("file%d.go", i)
	}
	p2 := commitMessagePrompt(manyFiles, "", "", "")
	if !strings.Contains(p2, "Changed files (125):") {
		t.Errorf("prompt missing total count:\n%s", p2)
	}
	if !strings.Contains(p2, "... and 25 more files") {
		t.Errorf("prompt missing truncation notice:\n%s", p2)
	}
	if !strings.Contains(p2, "- file0.go") || !strings.Contains(p2, "- file99.go") {
		t.Errorf("prompt missing head files:\n%s", p2)
	}
	if strings.Contains(p2, "- file100.go") {
		t.Errorf("prompt contains file beyond 100:\n%s", p2)
	}
}

// A commit message with no harness to write it is refused before anything
// reads or touches the repository, so the index stays as it was.
func TestGitCommitMessageWithoutHarnessLeavesIndex(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	gitTestRun(t, root, "reset", "-q")
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isolateSettings(t)
	m, err := newAgentManager(root, "", nil) // available, nothing selected
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	s.SetAgent(m)

	code, body := agentPost(t, s, "/api/git/commit-message")
	if code != 400 || !strings.Contains(fmt.Sprint(body["error"]), "no coding harness") {
		t.Fatalf("commit-message with no harness = %d %v, want 400 no coding harness", code, body)
	}
	if staged := gitStagedPaths(root); len(staged) != 0 {
		t.Fatalf("a refused commit message staged %v", staged)
	}
}

func TestParseCommitMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Thinking...\n<commit-message>\nFix the race\n\nWhy it raced.\n</commit-message>\n", "Fix the race\n\nWhy it raced."},
		{"<commit-message>```\nAdd x\n```</commit-message>", "Add x"},
		{"```text\nAdd y\n```", "Add y"},
		{"\"Add z\"", "Add z"},
		{"  Plain answer \n", "Plain answer"},
	}
	for _, c := range cases {
		if got := parseCommitMessage(c.in); got != c.want {
			t.Errorf("parseCommitMessage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseCommitPlan(t *testing.T) {
	changes := []gitChange{{"a.go", "M"}, {"b.go", "??"}, {"c.go", "D"}, {"d.go", "M"}}
	out := "echoed prompt <commit-plan>{\"commits\":[{\"message\":\"subject\\n\\nbody\",\"files\":[\"path\"]}]}</commit-plan>\n" +
		"answer:\n<commit-plan>\n```json\n" +
		`{"commits":[` +
		`{"message":"  Fix a \n\nbecause","files":["a.go","nope.go","b.go"]},` +
		`{"message":"Dup only","files":["a.go"]},` +
		`{"message":"Delete c","files":["c.go","nope.go"]}]}` +
		"\n```\n</commit-plan>"
	plan, err := parseCommitPlan(out, changes)
	if err != nil {
		t.Fatal(err)
	}
	want := CommitPlan{
		Commits: []CommitPlanGroup{
			{Message: "Fix a \n\nbecause", Files: []gitChange{{"a.go", "M"}, {"b.go", "??"}}},
			{Message: "Delete c", Files: []gitChange{{"c.go", "D"}}},
		},
		Unassigned: []gitChange{{"d.go", "M"}},
		Dropped:    []string{"nope.go"},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v\nwant %+v", plan, want)
	}

	for name, bad := range map[string]string{
		"no tags":       `{"commits":[{"message":"x","files":["a.go"]}]}`,
		"not json":      "<commit-plan>commit everything</commit-plan>",
		"empty message": `<commit-plan>{"commits":[{"message":" ","files":["a.go"]}]}</commit-plan>`,
		"nothing known": `<commit-plan>{"commits":[{"message":"x","files":["zzz"]}]}</commit-plan>`,
		"no commits":    `<commit-plan>{"commits":[]}</commit-plan>`,
	} {
		if _, err := parseCommitPlan(bad, changes); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// GET ?job= reads a finished prompt job; a running, failed, or file-editing
// one is refused with the reason.
func TestCommitPlanJobAnswer(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	isolateSettings(t)
	m, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	job := func(id int64, running bool, errMsg, stdout string, changed ...string) *agentJob {
		out := &tailBuffer{max: 1 << 16}
		out.Write([]byte(stdout))
		return &agentJob{ID: id, Running: running, Error: errMsg, Changed: changed, out: out}
	}
	plan := `<commit-plan>{"commits":[{"message":"Add","files":["add.go","untr.go"]}]}</commit-plan>`
	m.jobs = map[int64]*agentJob{
		1: job(1, false, "", plan),
		2: job(2, true, "", ""),
		3: job(3, false, "exit status 1", ""),
		4: job(4, false, "", plan, "keep.go", "sub/mod.go"),
		5: job(5, false, "", "<commit-message>Fix it</commit-message>"),
		6: job(6, false, "", "<commit-message> </commit-message>"),
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	s.SetAgent(m)

	code, body := get(t, s, "/api/git/commit-plan?job=1")
	commits, _ := body["commits"].([]any)
	unassigned, _ := body["unassigned"].([]any)
	if code != 200 || len(commits) != 1 || len(unassigned) != 4 {
		t.Fatalf("plan = %d %v", code, body)
	}
	for _, c := range []struct {
		url  string
		code int
		want string
	}{
		{"/api/git/commit-plan?job=2", 409, "still running"},
		{"/api/git/commit-plan?job=3", 422, "exit status 1"},
		{"/api/git/commit-plan?job=4", 422, "the harness edited files instead of only answering: keep.go, sub/mod.go"},
		{"/api/git/commit-plan?job=99", 404, "unknown job"},
		{"/api/git/commit-message?job=6", 422, "the harness returned an empty message"},
	} {
		if code, body := get(t, s, c.url); code != c.code || body["error"] != c.want {
			t.Errorf("%s = %d %v, want %d %q", c.url, code, body, c.code, c.want)
		}
	}
	if code, body := get(t, s, "/api/git/commit-message?job=5"); code != 200 || body["message"] != "Fix it" {
		t.Errorf("message = %d %v", code, body)
	}
}
