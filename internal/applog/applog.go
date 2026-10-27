// Package applog keeps punk0's log file: what the orchestrator, its workspace
// children and the agents' hooks did, for when something goes wrong on a
// machine nobody watches. Only the orchestrator (or a standalone PR review)
// owns the file and rotates it. A child logs to its stderr, which the
// orchestrator copies in (Child); a hook process adds a line on its own
// (Append).
//
// Nothing private goes in: no terminal output, prompts, file contents or
// environment. Every line is scrubbed of the home folder and of tokens that
// look like credentials (Redact) on its way to the file.
package applog

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// FileName is the log in Dir; older parts are FileName.1 and .2.
	FileName = "punk0.log"
	// CrashName is where the Go runtime writes a fatal panic's stack.
	CrashName = "crash.log"

	maxBytes = 5 << 20
	keep     = 2
	crashMax = 1 << 20
)

var (
	mu   sync.Mutex
	out  *rotator
	base *slog.Logger // no src attribute: Child sets its own
)

// Dir is the logs folder under punk0's state folder.
func Dir(state string) string { return filepath.Join(state, "logs") }

// Start sends slog's default logger, and the log package with it, to
// Dir(state)/punk0.log, rotated at 5 MiB with two older parts kept, and a
// fatal crash to crash.log. Lines from the log package still reach console
// too, as they did before there was a file. It returns the log's path.
func Start(state string, level slog.Level, console io.Writer) (string, error) {
	dir := Dir(state)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	r, err := openRotator(filepath.Join(dir, FileName), maxBytes, keep)
	if err != nil {
		return "", err
	}
	b := slog.New(slog.NewTextHandler(r, &slog.HandlerOptions{Level: level}))
	mu.Lock()
	out, base = r, b
	mu.Unlock()
	slog.SetDefault(b.With("src", "main"))
	// SetDefault routed the log package into the file only.
	log.SetFlags(0)
	log.SetOutput(consoleTee{console})
	startCrashOutput(dir)
	return r.path, nil
}

// Path is the log file Start opened, or "" before it or after it failed.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		return ""
	}
	return out.path
}

// Enabled reports whether this process writes the log file.
func Enabled() bool { return Path() != "" }

// consoleTee is the log package's output after Start: the line on the console,
// timestamped like the log package did, and as a record in the file.
type consoleTee struct{ w io.Writer }

func (t consoleTee) Write(p []byte) (int, error) {
	if t.w != nil {
		io.WriteString(t.w, time.Now().Format("2006/01/02 15:04:05 "))
		t.w.Write(p)
	}
	slog.Default().Info(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// Child copies one line a workspace child printed into the log. A child logs
// through slog's text format (time=… level=… msg=…), and such a line goes in
// as it is with the workspace added; anything else the child printed becomes
// the message of a record of its own.
func Child(ws, line string) {
	mu.Lock()
	r, b := out, base
	mu.Unlock()
	if r == nil || strings.TrimSpace(line) == "" {
		return
	}
	if strings.HasPrefix(line, "time=") && strings.Contains(line, " level=") {
		r.Write([]byte(line + " src=child ws=" + quote(ws) + "\n"))
		return
	}
	b.Info(line, "src", "child", "ws", ws)
}

// Append adds one record to the log of the punk0 whose state folder is state,
// without rotating it: the hook process is short-lived and not the file's
// owner. It fails silently, like everything the hook does.
func Append(state string, level slog.Level, msg string, args ...any) {
	dir := Dir(state)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Log(context.Background(), level, msg, append([]any{"src", "hook"}, args...)...)
	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	f.Write(Redact(buf.Bytes()))
	f.Close()
}

// Tail is the last n lines of the log, older part included when the current
// one has fewer.
func Tail(n int) ([]string, error) {
	p := Path()
	if p == "" {
		return nil, os.ErrNotExist
	}
	var lines []string
	for _, f := range []string{p, p + ".1"} {
		b, err := os.ReadFile(f)
		if err != nil {
			if f == p {
				return nil, err
			}
			break
		}
		part := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		lines = append(part, lines...)
		if len(lines) >= n {
			break
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// startCrashOutput has the runtime write a fatal error's stack to crash.log
// as well as stderr: on Windows the console holding stderr closes with punk0.
// A file grown past 1 MiB starts over.
func startCrashOutput(dir string) {
	p := filepath.Join(dir, CrashName)
	if st, err := os.Stat(p); err == nil && st.Size() > crashMax {
		os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	debug.SetCrashOutput(f, debug.CrashOptions{}) // it keeps its own copy of f
	f.Close()
}

// quote writes s the way slog's text format writes a value.
func quote(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r == '=' || r == '"' || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}
