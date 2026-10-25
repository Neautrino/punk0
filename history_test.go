package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// noCLI stands in for the agent binaries: a test must never run a real agent,
// and must never see the sessions of the machine it runs on.
type noCLI struct{}

func (noCLI) look(string) bool { return false }

func (noCLI) run(context.Context, string, string, ...string) ([]byte, error) {
	return nil, errors.New("no agent CLI in tests")
}

// chatHomes points every agent's history at its own temp folder, so a test
// never reads or deletes the chats of the machine it runs on; it answers the
// Claude config dir and the Codex home.
func chatHomes(t *testing.T) (string, string) {
	t.Helper()
	claude, codex := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	return claude, codex
}

// chatHome is chatHomes with a chatHistory reading those folders, with no
// agent binary behind it.
func chatHome(t *testing.T) (*chatHistory, string, string) {
	t.Helper()
	claude, codex := chatHomes(t)
	h := newChatHistory()
	h.cli = noCLI{}
	return h, claude, codex
}

// claudeUser is one prompt record as Claude Code writes it.
func claudeUser(dir, id, text string) string {
	return fmt.Sprintf(`{"type":"user","cwd":%q,"sessionId":%q,"userType":"external","message":{"role":"user","content":%q}}`, dir, id, text)
}

func claudeAssistantLine(dir, id string) string {
	return fmt.Sprintf(`{"type":"assistant","cwd":%q,"sessionId":%q,"message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`, dir, id)
}

// writeChat writes a transcript under <claude>/projects/<folder>/<id>.jsonl.
func writeChat(t *testing.T, claude, folder, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(claude, "projects", folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chatsByID is the scan as a map, so a test names the chat it means.
func chatsByID(h *chatHistory) map[string]chatSession {
	out := map[string]chatSession{}
	for _, c := range h.Sessions(context.Background()) {
		out[chatKey(c.Agent, c.ID)] = c
	}
	return out
}

func TestChatHistoryReadsClaudeTranscripts(t *testing.T) {
	h, claude, _ := chatHome(t)
	work, other := t.TempDir(), t.TempDir()

	// A chat whose title the user set, after one the model chose: the last
	// custom title wins over every ai title. Only the prompt and the reply
	// count as messages: a slash command, the caveat Claude inserts (isMeta)
	// and a tool result are not what was said.
	writeChat(t, claude, "-work", "aaaaaaaa-0000-4000-8000-000000000001",
		`{"type":"queue-operation","operation":"enqueue","sessionId":"aaaaaaaa-0000-4000-8000-000000000001","content":"/model"}`,
		claudeUser(work, "aaaaaaaa-0000-4000-8000-000000000001", "<command-name>/model</command-name>"),
		fmt.Sprintf(`{"type":"user","cwd":%q,"isMeta":true,"message":{"role":"user","content":"Caveat: this message was added automatically"}}`, work),
		claudeUser(work, "aaaaaaaa-0000-4000-8000-000000000001", "make the sidebar resizable"),
		fmt.Sprintf(`{"type":"user","cwd":%q,"toolUseResult":{"stdout":"ok"},"message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`, work),
		fmt.Sprintf(`{"type":"assistant","cwd":%q,"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`, work),
		claudeAssistantLine(work, "aaaaaaaa-0000-4000-8000-000000000001"),
		`{"type":"ai-title","aiTitle":"Resizable sidebar","sessionId":"aaaaaaaa-0000-4000-8000-000000000001"}`,
		`{"type":"custom-title","customTitle":"Sidebar work","sessionId":"aaaaaaaa-0000-4000-8000-000000000001"}`)
	// No title of any kind: the first real prompt names it.
	writeChat(t, claude, "-other", "bbbbbbbb-0000-4000-8000-000000000002",
		claudeUser(other, "bbbbbbbb-0000-4000-8000-000000000002", "why is the build slow?\nsecond line"))
	// A subagent's transcript belongs to a conversation, not to the list.
	sub := filepath.Join(claude, "projects", "-work", "aaaaaaaa-0000-4000-8000-000000000001", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-123.jsonl"), []byte(claudeUser(work, "agent-123", "helper")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	chats := chatsByID(h)
	if len(chats) != 2 {
		t.Fatalf("scan found %d chats, want the two transcripts: %+v", len(chats), chats)
	}
	first := chats["claude:aaaaaaaa-0000-4000-8000-000000000001"]
	if first.Title != "Sidebar work" {
		t.Errorf("title %q, want the custom title", first.Title)
	}
	if first.Dir != work {
		t.Errorf("dir %q, want the cwd inside the file (%q)", first.Dir, work)
	}
	if first.Messages != 2 {
		t.Errorf("messages %d, want the one prompt and one reply", first.Messages)
	}
	if first.Updated == 0 || first.File == "" {
		t.Errorf("chat without a time or a file: %+v", first)
	}
	if got := chats["claude:bbbbbbbb-0000-4000-8000-000000000002"].Title; got != "why is the build slow?" {
		t.Errorf("fallback title %q, want the first line of the first prompt", got)
	}

	// SessionsIn is what the Workspace and Project scopes narrow the list with.
	in := h.SessionsIn(work)
	if len(in) != 1 || in[0].ID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Fatalf("SessionsIn(%q) = %+v", work, in)
	}
	if n := len(h.SessionsIn(t.TempDir())); n != 0 {
		t.Fatalf("SessionsIn of a folder with no chats returned %d", n)
	}
}

// writeRollout writes one Codex rollout where it keeps them.
func writeRollout(t *testing.T, codex, day, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(codex, "sessions", day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-10-20T10-00-00-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChatHistoryReadsCodexRollouts(t *testing.T) {
	h, _, codex := chatHome(t)
	work := t.TempDir()
	const id = "019fc25a-490b-7ad3-b689-05046f31d1e7"

	rollout := writeRollout(t, codex, filepath.Join("2026", "10", "20"), id,
		fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"session_id":%q,"id":%q,"cwd":%q,"originator":"codex-tui"}}`, id, id, work),
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"follow the repo's conventions"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/x</cwd>\n</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"rename the branch"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"and push it"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`)
	// The index is written on rename, so it only wins while it is the newer
	// of the two.
	old := time.UnixMilli(1792368000000) // 2026-10-19T00:00:00Z
	if err := os.Chtimes(rollout, old, old); err != nil {
		t.Fatal(err)
	}
	// A rollout with no entry in the index keeps the prompt as its title.
	const plain = "019fc8c3-9d17-71d3-9bfe-6d7647b1e7c9"
	writeRollout(t, codex, filepath.Join("2026", "10", "21"), plain,
		fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}`, plain, work),
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a flag"}]}}`)
	// Renaming a thread appends a line; the last one for an id wins.
	index := strings.Join([]string{
		`{"id":"` + id + `","thread_name":"First name","updated_at":"2026-10-20T10:05:00.000000000Z"}`,
		`{"id":"` + id + `","thread_name":"Rename the branch","updated_at":"2026-10-22T09:00:00.000000000Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	chats := chatsByID(h)
	named := chats["codex:"+id]
	if named.Title != "Rename the branch" {
		t.Errorf("title %q, want the last name in session_index.jsonl", named.Title)
	}
	if named.Dir != work {
		t.Errorf("dir %q, want the cwd from session_meta (%q)", named.Dir, work)
	}
	if named.Messages != 3 {
		t.Errorf("messages %d, want the two prompts and the reply, without the developer instruction", named.Messages)
	}
	if want := int64(1792659600000); named.Updated != want { // 2026-10-22T09:00:00Z
		t.Errorf("updated %d, want the index's updated_at %d", named.Updated, want)
	}
	if got := chats["codex:"+plain]; got.Title != "add a flag" || got.Updated <= named.Updated {
		t.Errorf("rollout without an index entry %+v, want the first prompt and the file's own time", got)
	}

	// Renaming again shows up without the rollout changing.
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(index+`{"id":"`+id+`","thread_name":"Third name","updated_at":"2026-10-23T09:00:00.000000000Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := chatsByID(h)["codex:"+id].Title; got != "Third name" {
		t.Errorf("after a rename the title is %q", got)
	}
}

// fakeCLI answers for one binary and records the folders it was asked in. Its
// answer can change between rounds, and the binary can leave PATH, as an
// uninstall would.
type fakeCLI struct {
	mu   sync.Mutex
	out  []byte
	err  error
	gone bool
	runs int
	dirs []string
}

func (f *fakeCLI) look(bin string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return bin == "opencode" && !f.gone
}

func (f *fakeCLI) run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs++
	f.dirs = append(f.dirs, dir)
	return f.out, f.err
}

// answer is what the next rounds get back.
func (f *fakeCLI) answer(out []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out, f.err = out, err
}

// uninstall takes the binary off PATH.
func (f *fakeCLI) uninstall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone = true
}

func (f *fakeCLI) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs
}

func (f *fakeCLI) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(slices.Values(f.dirs))
}

func TestChatHistoryAsksOpenCodeInTheBackground(t *testing.T) {
	h, _, _ := chatHome(t)
	work, other := t.TempDir(), t.TempDir()
	// The CLI lists the project of the folder it runs in, so both folders
	// answer the same two sessions: the rows are deduped by id.
	cli := &fakeCLI{out: []byte(`[
	  {"id":"ses_1","title":"Port the parser","updated":1792000000000,"created":1791990000000,"projectId":"prj_1","directory":"` + work + `"},
	  {"id":"ses_2","title":"","updated":1792000001000,"created":1791990001000,"projectId":"prj_1","directory":"` + work + `"}
	]`)}
	h.cli = cli
	h.dirs = func() []string { return []string{work, other} }

	// The first scan must not wait on a round of processes.
	if n := len(h.Sessions(context.Background())); n != 0 {
		t.Fatalf("the first scan returned %d OpenCode chats; it must not block on the CLI", n)
	}
	// Waiting for the background round to answer.
	waitFor(t, func() bool { return len(h.Sessions(context.Background())) == 2 })

	chats := chatsByID(h)
	one := chats["opencode:ses_1"]
	if one.Title != "Port the parser" || one.Dir != work || one.Updated != 1792000000000 {
		t.Errorf("opencode chat %+v", one)
	}
	if one.File != "" {
		t.Errorf("opencode chat has a file %q; its sessions are in a database", one.File)
	}
	if one.Messages != 0 {
		t.Errorf("messages %d, want none: the listing holds no count", one.Messages)
	}
	if got := chats["opencode:ses_2"].Title; got != "(untitled)" {
		t.Errorf("untitled session reads %q", got)
	}
	if got := cli.asked(); !slices.Equal(got, slices.Sorted(slices.Values([]string{work, other}))) {
		t.Errorf("asked in %v, want every folder punk0 knows", got)
	}

	// Further scans use the answer: the CLI is not run per request.
	before := cli.calls()
	for range 5 {
		h.Sessions(context.Background())
	}
	if cli.calls() != before {
		t.Fatalf("opencode was asked %d more times for five scans", cli.calls()-before)
	}

	// With opencode not installed nothing runs at all.
	h2, _, _ := chatHome(t)
	if n := len(h2.Sessions(context.Background())); n != 0 {
		t.Fatalf("a machine without opencode listed %d chats", n)
	}
}

// chatIDs is the ids one scan listed, in the order they came.
func chatIDs(h *chatHistory) []string {
	var out []string
	for _, c := range h.Sessions(context.Background()) {
		out = append(out, c.ID)
	}
	return out
}

// An id comes out of OpenCode's own JSON and goes straight back onto a
// command line (`session export <id>`, `session delete <id>`) and into a
// tab's command, so one that could pass itself off as a flag or a path is
// not a chat punk0 lists at all.
func TestChatHistoryLeavesOutOpenCodeIdsThatAreNotSessionIds(t *testing.T) {
	h, _, _ := chatHome(t)
	work := t.TempDir()
	cli := &fakeCLI{out: []byte(`[
	  {"id":"--force","title":"a flag","updated":1792000000000,"directory":"` + work + `"},
	  {"id":"-rf","title":"another flag","updated":1792000001000,"directory":"` + work + `"},
	  {"id":"../../etc/passwd","title":"a path","updated":1792000002000,"directory":"` + work + `"},
	  {"id":"","title":"no id at all","updated":1792000003000,"directory":"` + work + `"},
	  {"id":"ses_ok","title":"a real one","updated":1792000004000,"directory":"` + work + `"}
	]`)}
	h.cli = cli
	h.dirs = func() []string { return []string{work} }

	h.askOpenCode() // one round, in hand
	if got := chatIDs(h); !slices.Equal(got, []string{"ses_ok"}) {
		t.Fatalf("listed %q, want only the id that is one", got)
	}
}

// A round keeps the last rows only when it learnt nothing: a folder that
// answered nothing has no chats, and an opencode that left PATH answers
// nothing ever again, so neither may leave stale rows listed.
func TestChatHistoryOpenCodeEmptyAnswerEmptiesTheList(t *testing.T) {
	h, _, _ := chatHome(t)
	work := t.TempDir()
	const rows = `[{"id":"ses_1","title":"Port the parser","updated":1792000000000,"directory":"%s"}]`
	cli := &fakeCLI{out: fmt.Appendf(nil, rows, work)}
	h.cli = cli
	h.dirs = func() []string { return []string{work} }

	h.askOpenCode()
	if got := chatIDs(h); !slices.Equal(got, []string{"ses_1"}) {
		t.Fatalf("listed %q, want the row the CLI answered", got)
	}

	// A round that failed outright: an upgrade passing through, a locked
	// database. The last good rows stay.
	cli.answer(nil, errors.New("opencode: database is locked"))
	h.askOpenCode()
	if got := chatIDs(h); !slices.Equal(got, []string{"ses_1"}) {
		t.Fatalf("a failed round left %q, want the last good rows", got)
	}

	// Nothing on stdout is a correct answer: this folder has no chats.
	cli.answer(nil, nil)
	h.askOpenCode()
	if got := chatIDs(h); len(got) != 0 {
		t.Fatalf("after an empty answer the list still holds %q", got)
	}

	// And a binary that is gone takes its rows with it rather than leaving
	// them listed until punk0 restarts.
	cli.answer(fmt.Appendf(nil, rows, work), nil)
	h.askOpenCode()
	if got := chatIDs(h); !slices.Equal(got, []string{"ses_1"}) {
		t.Fatalf("listed %q after the CLI answered again", got)
	}
	cli.uninstall()
	if got := chatIDs(h); len(got) != 0 {
		t.Fatalf("opencode left PATH and its chats are still listed: %q", got)
	}
	before := cli.calls()
	h.Sessions(context.Background())
	if cli.calls() != before {
		t.Fatal("a round was started for a binary that is not installed")
	}
}

func TestChatHistoryFollowsTheFilesOnDisk(t *testing.T) {
	h, claude, _ := chatHome(t)
	work := t.TempDir()
	const id = "cccccccc-0000-4000-8000-000000000003"
	lines := []string{claudeUser(work, id, "first question")}
	path := writeChat(t, claude, "-work", id, lines...)

	first := chatsByID(h)["claude:"+id]
	if first.Messages != 1 || first.Title != "first question" {
		t.Fatalf("first scan: %+v", first)
	}

	// The conversation goes on: the chat the page lists must go with it.
	lines = append(lines, claudeAssistantLine(work, id),
		`{"type":"ai-title","aiTitle":"Build times","sessionId":"`+id+`"}`)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	grown := chatsByID(h)["claude:"+id]
	if grown.Messages != 2 || grown.Title != "Build times" {
		t.Fatalf("after the transcript grew: %+v", grown)
	}
	if grown.Updated <= first.Updated {
		t.Fatalf("updated did not move: %d then %d", first.Updated, grown.Updated)
	}

	// A transcript deleted outside punk0 leaves the list at once.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := chatsByID(h)["claude:"+id]; ok {
		t.Fatal("a deleted transcript is still listed")
	}
	if len(h.files) != 0 {
		t.Fatalf("the cache kept %d entries for files that are gone", len(h.files))
	}
}

// Claude Code has no delete command, so punk0 unlinks the transcript and the
// <id>/ folder of subagent transcripts beside it -- and nothing else, nothing
// to a trash folder.
func TestChatHistoryDeleteClaudeUnlinksItsOwnFiles(t *testing.T) {
	h, claude, _ := chatHome(t)
	state := t.TempDir()
	work := t.TempDir()
	const id = "dddddddd-0000-4000-8000-000000000004"
	path := writeChat(t, claude, "-work", id, claudeUser(work, id, "look at this"))
	// Subagent transcripts belong to this conversation and go with it.
	side := filepath.Join(claude, "projects", "-work", id)
	if err := os.MkdirAll(side, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(side, "agent-1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A chat of another folder must survive its neighbour's delete.
	const other = "eeeeeeee-0000-4000-8000-000000000005"
	kept := writeChat(t, claude, "-work", other, claudeUser(work, other, "keep me"))

	chat := chatsByID(h)["claude:"+id]
	if chat.ID == "" {
		t.Fatal("the chat was not listed before the delete")
	}
	if err := h.Delete(chat); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the transcript is still there: %v", err)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("the subagents folder is still there: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the other chat of that folder went too: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "trash")); !os.IsNotExist(err) {
		t.Fatalf("a chat was put in punk0's trash: %v", err)
	}
	if _, ok := chatsByID(h)["claude:"+id]; ok {
		t.Fatal("the deleted chat is still listed")
	}
}

// Every permanent delete is checked against what a scan can have found: an
// id that is not one, a transcript outside Claude Code's own folders, a file
// that is not named after its chat. A mismatch is an error, never a guess.
func TestChatHistoryDeleteRefusesPathsItDidNotScan(t *testing.T) {
	h, claude, _ := chatHome(t)
	outside := filepath.Join(t.TempDir(), "secrets.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const id = "ffffffff-0000-4000-8000-000000000006"
	deep := filepath.Join(claude, "projects", "-work", "nested")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(deep, id+".jsonl")
	if err := os.WriteFile(nested, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	renamed := writeChat(t, claude, "-work", "not-the-id", claudeUser(t.TempDir(), id, "hello"))

	for _, tc := range []struct {
		name string
		chat chatSession
		path string
	}{
		{"an id that is not one", chatSession{Agent: tabClaude, ID: "../../x", File: outside}, outside},
		{"a transcript outside the agent's folders", chatSession{Agent: tabClaude, ID: id, File: outside}, outside},
		{"a transcript two folders down", chatSession{Agent: tabClaude, ID: id, File: nested}, nested},
		{"a file not named after its chat", chatSession{Agent: tabClaude, ID: id, File: renamed}, renamed},
	} {
		if err := h.Delete(tc.chat); err == nil {
			t.Errorf("%s: deleted %s", tc.name, tc.path)
		}
		if _, err := os.Stat(tc.path); err != nil {
			t.Errorf("%s: %s is gone: %v", tc.name, tc.path, err)
		}
	}
}

func TestChatHistoryDeleteUsesTheAgentsOwnCommand(t *testing.T) {
	h, _, _ := chatHome(t)
	work := t.TempDir()
	var got []string
	h.cli = &recordCLI{out: []byte(`[]`), seen: &got}

	if err := h.Delete(chatSession{Agent: "codex", ID: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Delete(chatSession{Agent: "opencode", ID: "ses_9", Dir: work}); err != nil {
		t.Fatal(err)
	}
	want := []string{"codex delete abc", "opencode session delete ses_9 [in " + work + "]"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ran %q, want %q", got, want)
	}
	if err := h.Delete(chatSession{Agent: "agy", ID: "x"}); err == nil {
		t.Fatal("an agent with no delete command reported success")
	}
	if err := h.Delete(chatSession{Agent: "codex", ID: "../../etc"}); err == nil {
		t.Fatal("an id that is not a session id was passed to the agent")
	}
	if err := h.Delete(chatSession{Agent: "codex", ID: "-rf"}); err == nil {
		t.Fatal("an id that could pass itself off as a flag was passed to the agent")
	}
}

// recordCLI records the command lines it was asked to run, with the folder
// when it was asked to run in one.
type recordCLI struct {
	out  []byte
	seen *[]string
}

func (recordCLI) look(string) bool { return true }

func (c *recordCLI) run(_ context.Context, dir, bin string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{bin}, args...), " ")
	if dir != "" {
		line += " [in " + dir + "]"
	}
	*c.seen = append(*c.seen, line)
	return c.out, nil
}

// The preview is the conversation as the expanded row shows it: the last
// real prompt (slash commands, meta records and tool results are nobody's
// prompt), the whole reply after it -- every text block of every assistant
// record, tool calls between them skipped -- how the chat started, and the
// branch the records carry.
func TestChatPreviewReadsClaudeTranscripts(t *testing.T) {
	h, claude, _ := chatHome(t)
	work := t.TempDir()
	const id = "aaaaaaaa-1111-4000-8000-000000000001"
	asst := func(branch, content string) string {
		return fmt.Sprintf(`{"type":"assistant","cwd":%q,"gitBranch":%q,"message":{"role":"assistant","content":[%s]}}`, work, branch, content)
	}
	writeChat(t, claude, "-work", id,
		claudeUser(work, id, "<command-name>/model</command-name>"),
		fmt.Sprintf(`{"type":"user","cwd":%q,"isMeta":true,"gitBranch":"main","message":{"role":"user","content":"Caveat: added automatically"}}`, work),
		fmt.Sprintf(`{"type":"user","cwd":%q,"gitBranch":"main","message":{"role":"user","content":"port the parser"}}`, work),
		asst("main", `{"type":"text","text":"parser ported"}`),
		fmt.Sprintf(`{"type":"user","cwd":%q,"gitBranch":"feat","message":{"role":"user","content":"and now the lexer"}}`, work),
		asst("feat", `{"type":"text","text":"Looking at the lexer."}`),
		asst("feat", `{"type":"tool_use","name":"Bash","input":{}}`),
		fmt.Sprintf(`{"type":"user","cwd":%q,"toolUseResult":{"stdout":"ok"},"message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`, work),
		fmt.Sprintf(`{"type":"user","cwd":%q,"isSidechain":true,"message":{"role":"user","content":"subagent, go look"}}`, work),
		claudeUser(work, id, "/compact"),
		asst("feat", `{"type":"thinking","thinking":"hm"}`),
		asst("feat", `{"type":"text","text":"lexer done"}`))

	c := chatsByID(h)["claude:"+id]
	p, err := h.Preview(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.LastPrompt != "and now the lexer" {
		t.Errorf("last prompt %q, want the last one the user typed", p.LastPrompt)
	}
	if p.LastReply != "Looking at the lexer.\n\nlexer done" {
		t.Errorf("last reply %q, want both text records joined, the tool call left out", p.LastReply)
	}
	if p.Started != "" {
		t.Errorf("started %q, want none: it is the title", p.Started)
	}
	if c.Title != "port the parser" {
		t.Fatalf("title %q", c.Title)
	}
	if p.Branch != "feat" {
		t.Errorf("branch %q, want the last one in the transcript", p.Branch)
	}
	// Messages is the list's own count, which takes the sidechain prompt in;
	// the preview does not, since a subagent is not talking to the user.
	if p.Dir != work || p.Messages != 7 {
		t.Errorf("preview %+v, want the folder and the list's message count", p)
	}

	// A second read answers from the cache.
	if _, err := h.Preview(chatsByID(h)["claude:"+id]); err != nil {
		t.Fatal(err)
	}
	if n := len(h.previews); n != 1 {
		t.Fatalf("the preview cache holds %d entries", n)
	}

	// Long text is cut, not dropped.
	const long = "bbbbbbbb-1111-4000-8000-000000000002"
	writeChat(t, claude, "-work", long, claudeUser(work, long, strings.Repeat("x", previewMax+50)))
	p2, err := h.Preview(chatsByID(h)["claude:"+long])
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(p2.LastPrompt)) != previewMax+1 || !strings.HasSuffix(p2.LastPrompt, "…") {
		t.Errorf("a long prompt reads %d characters", len([]rune(p2.LastPrompt)))
	}
}

// A chat that ends on a prompt shows it with no reply; a greeting is not how
// the chat started; a detached head names no branch; and the opening prompt
// is shown only when it is neither the title nor the last prompt.
func TestChatPreviewStartedAndTrailingPrompt(t *testing.T) {
	h, claude, _ := chatHome(t)
	work := t.TempDir()
	line := func(typ, branch, content string) string {
		return fmt.Sprintf(`{"type":%q,"cwd":%q,"gitBranch":%q,"message":{"role":%q,"content":%s}}`, typ, work, branch, typ, content)
	}
	const greet = "dddddddd-1111-4000-8000-000000000004"
	writeChat(t, claude, "-work", greet,
		line("user", "HEAD", `"hi"`),
		line("assistant", "HEAD", `[{"type":"text","text":"Hello!"}]`),
		line("user", "HEAD", `"fix the flaky test"`))
	p, err := h.Preview(chatsByID(h)["claude:"+greet])
	if err != nil {
		t.Fatal(err)
	}
	if p.LastPrompt != "fix the flaky test" || p.LastReply != "" {
		t.Errorf("exchange %q / %q, want the trailing prompt and no reply", p.LastPrompt, p.LastReply)
	}
	if p.Started != "" || p.Branch != "" {
		t.Errorf("started %q, branch %q: want neither for a greeting on a detached head", p.Started, p.Branch)
	}

	// The opening prompt, cut short in the title, is shown whole.
	const opened = "eeeeeeee-1111-4000-8000-000000000005"
	first := "port the parser from the old grammar to the new one, keeping every test green"
	writeChat(t, claude, "-work", opened,
		line("user", "main", fmt.Sprintf("%q", first)),
		line("assistant", "main", `[{"type":"text","text":"ported"}]`),
		line("user", "main", `"now the lexer"`),
		line("assistant", "main", `[{"type":"text","text":"done"}]`))
	p, err = h.Preview(chatsByID(h)["claude:"+opened])
	if err != nil {
		t.Fatal(err)
	}
	if p.Started != first || p.LastPrompt != "now the lexer" || p.LastReply != "done" || p.Branch != "main" {
		t.Errorf("preview %+v", p)
	}

	// One prompt is the title and the last prompt both: no Started.
	const single = "ffffffff-1111-4000-8000-000000000006"
	writeChat(t, claude, "-work", single,
		line("user", "main", fmt.Sprintf("%q", first)),
		line("assistant", "main", `[{"type":"text","text":"ported"}]`))
	p, err = h.Preview(chatsByID(h)["claude:"+single])
	if err != nil {
		t.Fatal(err)
	}
	if p.Started != "" || p.LastPrompt != first {
		t.Errorf("preview %+v, want no started for the only prompt", p)
	}
}

func TestChatPreviewReadsCodexRollouts(t *testing.T) {
	h, _, codex := chatHome(t)
	work := t.TempDir()
	const id = "019fc25a-490b-7ad3-b689-05046f31d1e8"
	writeRollout(t, codex, filepath.Join("2026", "10", "20"), id,
		fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}`, id, work),
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"follow the conventions"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/x</cwd>\n</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"rename the branch"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"renamed"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"and push it"}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"shell"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"pushing"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"pushed"}]}}`)

	p, err := h.Preview(chatsByID(h)["codex:"+id])
	if err != nil {
		t.Fatal(err)
	}
	// "rename the branch" is the title, so it is not shown again as the start.
	if p.LastPrompt != "and push it" || p.LastReply != "pushing\n\npushed" || p.Started != "" {
		t.Errorf("preview %+v, want the last exchange, not the developer instruction", p)
	}
	if p.Branch != "" {
		t.Errorf("branch %q, want none: Codex rollouts carry no branch", p.Branch)
	}
}

func TestChatPreviewExportsOpenCodeSessions(t *testing.T) {
	h, _, _ := chatHome(t)
	work := t.TempDir()
	var got []string
	h.cli = &recordCLI{seen: &got, out: []byte(`{
	  "info": {"id": "ses_7", "title": "Port the parser", "location": {"directory": "` + work + `"}},
	  "messages": [
	    {"type": "user", "text": "port the parser"},
	    {"type": "assistant", "content": [{"type": "text", "text": "ported"}]},
	    {"type": "idle"},
	    {"type": "user", "text": "now the lexer"},
	    {"type": "assistant", "content": [{"type": "reasoning", "text": "thinking"}, {"type": "text", "text": "lexing"}, {"type": "tool", "text": "x"}, {"type": "text", "text": "lexed"}]}
	  ]
	}`)}

	chat := chatSession{Agent: "opencode", ID: "ses_7", Dir: work, Updated: 1792000000000}
	p, err := h.Preview(chat)
	if err != nil {
		t.Fatal(err)
	}
	// No title was given, so the opening prompt is shown as the start.
	if p.LastPrompt != "now the lexer" || p.LastReply != "lexing\n\nlexed" || p.Started != "port the parser" {
		t.Errorf("preview %+v, want the text blocks after the last prompt", p)
	}
	if p.Dir != work || p.Messages != 0 {
		t.Errorf("preview %+v", p)
	}
	if len(got) != 1 || got[0] != "opencode session export ses_7 [in "+work+"]" {
		t.Fatalf("ran %q", got)
	}
	// The second read is cached: a preview costs one process, not one per open.
	if _, err := h.Preview(chat); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("the CLI ran again: %q", got)
	}
}

// A transcript past previewWhole is read as its head and its tail, so the
// first prompt and the last exchange are both there and the middle is not.
func TestChatPreviewReadsHeadAndTailOfAHugeTranscript(t *testing.T) {
	h, claude, _ := chatHome(t)
	work := t.TempDir()
	const id = "cccccccc-1111-4000-8000-000000000003"
	lines := []string{claudeUser(work, id, "the very first prompt")}
	// Filler past previewWhole, each line its own record.
	filler := claudeUser(work, id, strings.Repeat("f", 64<<10))
	for range (previewWhole / len(filler)) + 2 {
		lines = append(lines, filler)
	}
	lines = append(lines, claudeUser(work, id, "the very last prompt"),
		fmt.Sprintf(`{"type":"assistant","cwd":%q,"message":{"role":"assistant","content":[{"type":"text","text":"the very last reply"}]}}`, work))
	path := writeChat(t, claude, "-work", id, lines...)
	info, err := os.Stat(path)
	if err != nil || info.Size() <= previewWhole {
		t.Fatalf("the fixture is %+v bytes, want more than previewWhole: %v", info, err)
	}

	p, err := h.Preview(chatsByID(h)["claude:"+id])
	if err != nil {
		t.Fatal(err)
	}
	if p.LastPrompt != "the very last prompt" || p.LastReply != "the very last reply" {
		t.Errorf("preview %+v", p)
	}
}
