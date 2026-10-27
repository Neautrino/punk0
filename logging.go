package main

// punk0's log file (internal/applog): the orchestrator writes it, a
// workspace child logs to its stderr for the orchestrator to copy in, and the
// agents page offers it through Open logs folder and Copy diagnostics.

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"px0/internal/applog"
	"px0/internal/ui"
)

// diagnosticsLines is how much of the log Copy diagnostics takes.
const diagnosticsLines = 200

func logLevel() slog.Level {
	if ui.Verbose {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// startLog opens the log file for a punk0 that owns it: the orchestrator or a
// standalone PR review. A log that cannot be opened is said once and skipped.
func startLog() {
	if _, err := applog.Start(workbenchStateDir(), logLevel(), os.Stderr); err != nil {
		ui.Status("warn", "no log file: "+err.Error(), "", 0, os.Stderr)
	}
}

// startChildLog has a workspace child log to its stderr, which its
// orchestrator copies into the log file.
func startChildLog() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()})))
}

// startupAttrs say which punk0 runs where: the log's first record and the
// head of Copy diagnostics.
func startupAttrs(addr string) []any {
	claude, _ := exec.LookPath("claude")
	git, _ := exec.LookPath("git")
	return []any{
		"version", version,
		"os", runtime.GOOS + "/" + runtime.GOARCH,
		"osVersion", osVersion(),
		"go", runtime.Version(),
		"pid", os.Getpid(),
		"addr", addr,
		"state", tildePath(workbenchStateDir()),
		"shell", terminalShell(),
		"claude", orNone(tildePath(claude)),
		"git", orNone(tildePath(git)),
		"args", strings.Join(os.Args[1:], " "),
	}
}

func orNone(s string) string {
	if s == "" {
		return "(not found)"
	}
	return s
}

// logName is how the log names a workspace child: its folder's name, or the
// pull request.
func logName(it wbItem) string {
	if it.Kind == "pr" {
		return fmt.Sprintf("%s#%d", it.Repo, it.Number)
	}
	return filepath.Base(it.Path)
}

// notResponding says why a request for workspace id got no answer -- what
// made its child fail, or that it is still starting -- and where the log is.
func (wb *workbench) notResponding(id string) string {
	wb.mu.Lock()
	state, reason := "", ""
	if c := wb.children[id]; c != nil {
		state, reason = c.state, c.err
	}
	wb.mu.Unlock()
	msg := "workspace is not responding"
	switch {
	case reason != "":
		msg += ": " + reason
	case state == "starting":
		msg += fmt.Sprintf(": still starting after %s", wbAPIWait)
	}
	if p := applog.Path(); p != "" {
		msg += " (details in " + p + ")"
	}
	return msg
}

// handleDiagnostics serves GET /api/diagnostics: what to paste into a bug
// report. It names folders and says what punk0 did, so like the terminals it
// is served only while punk0 listens on this machine.
func (wb *workbench) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !wb.localOnly {
		fail(w, http.StatusForbidden, "diagnostics are served only when punk0 listens on this machine")
		return
	}
	if !trustedGet(w, r) {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "punk0 diagnostics, %s\n\n", time.Now().Format(time.RFC3339))
	attrs := startupAttrs(r.Host)
	for i := 0; i+1 < len(attrs); i += 2 {
		fmt.Fprintf(&b, "%-10s %v\n", attrs[i], attrs[i+1])
	}
	logPath := applog.Path()
	fmt.Fprintf(&b, "%-10s %s\n", "log", orNone(tildePath(logPath)))
	crash := "(none)"
	if st, err := os.Stat(filepath.Join(applog.Dir(workbenchStateDir()), applog.CrashName)); err == nil && st.Size() > 0 {
		crash = fmt.Sprintf("%s, %d bytes, written %s", applog.CrashName, st.Size(), st.ModTime().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "%-10s %s\n", "crash", crash)

	b.WriteString("\nworkspaces\n")
	wb.mu.Lock()
	n := 0
	for id, c := range wb.children {
		n++
		line := fmt.Sprintf("  %s  %s", id, c.state)
		if c.err != "" {
			line += "  " + c.err
		}
		b.WriteString(line + "\n")
	}
	wb.mu.Unlock()
	if n == 0 {
		b.WriteString("  (none started)\n")
	}

	fmt.Fprintf(&b, "\nlast %d lines of the log\n", diagnosticsLines)
	lines, err := applog.Tail(diagnosticsLines)
	if err != nil {
		fmt.Fprintf(&b, "  (cannot read the log: %v)\n", err)
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	writeJSON(w, map[string]any{"text": string(applog.Redact([]byte(b.String()))), "log": logPath})
}

// handleLogsOpen serves POST /api/logs/open: the logs folder in the system's
// file manager. The folder's path comes back either way, for the page to show
// when no file manager would start.
func (wb *workbench) handleLogsOpen(w http.ResponseWriter, r *http.Request) {
	if !wb.localOnly {
		fail(w, http.StatusForbidden, "the logs folder opens only when punk0 listens on this machine")
		return
	}
	if !localPost(w, r) {
		return
	}
	dir := applog.Dir(workbenchStateDir())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := openFolder(dir); err != nil {
		fail(w, http.StatusInternalServerError, err.Error()+": the logs are in "+dir)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "dir": dir})
}

// openFolder shows dir in the system's file manager.
func openFolder(dir string) error {
	var cmds []*exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmds = []*exec.Cmd{exec.Command("explorer.exe", dir)}
	case "darwin":
		cmds = []*exec.Cmd{exec.Command("open", dir)}
	default:
		cmds = []*exec.Cmd{exec.Command("xdg-open", dir), exec.Command("gio", "open", dir)}
	}
	for _, cmd := range cmds {
		if cmd.Start() == nil {
			go cmd.Wait()
			return nil
		}
	}
	return errors.New("no file manager would start")
}
