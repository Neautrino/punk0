//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Codex trusts a hook by a hash of its definition; punk0 trusts its own hooks
// for a run by passing that hash. This one was taken from Codex 0.166 running
// a hook it trusted through exactly this override.
func TestCodexHookHashMatchesCodex(t *testing.T) {
	got := codexHookHash("session_start", "cat >> /tmp/cx-hook.log; echo >> /tmp/cx-hook.log")
	if want := "sha256:145ffdc562ec63f219257bd5f05e34e712fed71fe540f089b69cbc916c7a34eb"; got != want {
		t.Fatalf("hash %s, want %s", got, want)
	}
}

// An agent typed at a tab's prompt reports its start and its exit, with the
// hooks punk0 adds, whatever order the user's rc files put PATH in; one that
// is not installed only says it is gone, so the tab never shows it.
func TestAgentFunctionsReportStartAndExit(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("ZDOTDIR", "")
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			useSettings(t, map[string]any{})

			// The user's rc puts their own bin first, after punk0 set PATH.
			bin, log := filepath.Join(home, "bin"), filepath.Join(home, "hook.log")
			os.MkdirAll(bin, 0o755)
			writeExec(t, filepath.Join(bin, "codex"), "#!/bin/sh\necho \"codex ran with $#: $*\" >> "+log+"\nexit 3\n")
			rc := "export PATH=" + bin + ":$PATH\nPS1='$ '\n"
			for _, f := range []string{".zshrc", ".bashrc"} {
				os.WriteFile(filepath.Join(home, f), []byte(rc), 0o644)
			}
			// The hook command stands in for punk0: it logs what it is told.
			exe := filepath.Join(home, "punk0")
			writeExec(t, exe, "#!/bin/sh\necho \"$*\" >> "+log+"\n")
			if err := writeAgentShell(exe); err != nil {
				t.Fatal(err)
			}

			sh, err := startShell(home, path, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sh.Close)
			waitShown(t, sh, "$ ")
			sh.tty.Write([]byte("codex resume x; cursor-agent; echo done-$?\r"))
			waitShown(t, sh, "done-127")

			got, _ := os.ReadFile(log)
			lines := strings.Split(strings.TrimSpace(string(got)), "\n")
			if len(lines) != 4 ||
				lines[0] != "hook start codex" ||
				!strings.HasPrefix(lines[1], "codex ran with ") || !strings.Contains(lines[1], "hooks.state=") || !strings.HasSuffix(lines[1], " resume x") ||
				lines[2] != "hook exit codex 3" ||
				lines[3] != "hook exit cursor-agent 127" {
				t.Fatalf("log:\n%s", got)
			}
		})
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// waitShown waits until the shell's screen shows want.
func waitShown(t *testing.T, sh *Shell, want string) {
	t.Helper()
	waitFor(t, func() bool {
		sh.mu.Lock()
		defer sh.mu.Unlock()
		return strings.Contains(bufferText(sh.screen), want)
	})
}

// An agent whose reporting punk0 could not write this run starts without it:
// the file on the line would be whatever an earlier punk0 left there, which
// reports to a punk0 that is gone. The next run that can write it gets it
// back.
func TestAgentRunsUnhookedWhenItsSetupFails(t *testing.T) {
	useSettings(t, map[string]any{})
	exe := filepath.Join(t.TempDir(), "punk0")
	// A failed setup belongs to one punk0 run, and the tests share a process.
	t.Cleanup(func() {
		reportOff.mu.Lock()
		reportOff.kind = nil
		reportOff.mu.Unlock()
	})

	// Nothing can be written where a folder already stands.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(claudeHooksPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentShell(exe); err != nil {
		t.Fatalf("one agent's setup failing stopped the others: %v", err)
	}
	if got, want := agentCommand(tabClaude, "S", true, false, posixShell), "claude --session-id S"; got != want {
		t.Fatalf("claude with no hook settings\n got %s\nwant %s", got, want)
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := writeAgentShell(exe); err != nil {
		t.Fatal(err)
	}
	want := "claude --settings " + shellQuote(claudeHooksPath()) + " --session-id S"
	if got := agentCommand(tabClaude, "S", true, false, posixShell); got != want {
		t.Fatalf("claude with hook settings\n got %s\nwant %s", got, want)
	}
}

// A qwen typed at a tab's prompt gets the defaults file punk0 wrote for it in
// its own environment, and droid its settings file on its command line; the
// shell left at the prompt carries neither.
func TestAgentFunctionsCarryTheirReporting(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("ZDOTDIR", "")
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			useSettings(t, map[string]any{})

			bin, log := filepath.Join(home, "bin"), filepath.Join(home, "agent.log")
			os.MkdirAll(bin, 0o755)
			for _, name := range []string{"qwen", "droid"} {
				writeExec(t, filepath.Join(bin, name), "#!/bin/sh\necho \""+name+" env=${QWEN_CODE_SYSTEM_DEFAULTS_PATH-unset} args=$*\" >> "+log+"\n")
			}
			rc := "export PATH=" + bin + ":$PATH\nPS1='$ '\n"
			for _, f := range []string{".zshrc", ".bashrc"} {
				os.WriteFile(filepath.Join(home, f), []byte(rc), 0o644)
			}
			exe := filepath.Join(home, "punk0")
			writeExec(t, exe, "#!/bin/sh\n")
			if err := writeAgentShell(exe); err != nil {
				t.Fatal(err)
			}

			sh, err := startShell(home, path, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sh.Close)
			waitShown(t, sh, "$ ")
			sh.tty.Write([]byte("qwen x; droid y; echo \"left=${QWEN_CODE_SYSTEM_DEFAULTS_PATH-unset}\"\r"))
			waitShown(t, sh, "left=unset")

			defaults := filepath.Join(reportDir("qwen"), "defaults.json")
			settings := filepath.Join(reportDir("droid"), "droid.json")
			got, _ := os.ReadFile(log)
			if want := "qwen env=" + defaults + " args=x\ndroid env=unset args=--settings " + settings + " y\n"; string(got) != want {
				t.Fatalf("log:\n%s\nwant:\n%s", got, want)
			}
			for _, f := range []string{defaults, settings} {
				if _, err := os.Stat(f); err != nil {
					t.Fatalf("an agent is pointed at %s, which punk0 did not write: %v", f, err)
				}
			}
		})
	}
}

// Where punk0 cannot hook the shell (sh here, PowerShell on Windows), a qwen
// tab carries the variable its function would set in the tab's environment
// instead; a tab of an agent that reports another way does not.
func TestUnhookedTabCarriesItsAgentsReportingEnv(t *testing.T) {
	wb, _, repo := historyWorkbench(t) // SHELL=/bin/sh
	if err := writeAgentShell(filepath.Join(t.TempDir(), "punk0")); err != nil {
		t.Fatal(err)
	}
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "agent.log")
	for _, name := range []string{"qwen", "codex"} {
		writeExec(t, filepath.Join(bin, name), "#!/bin/sh\necho \""+name+" env=${QWEN_CODE_SYSTEM_DEFAULTS_PATH-unset}\" >> "+log+"\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"qwen", "codex"} {
		tab, err := wb.records.AddTab(p.Workspaces[0].ID, kind, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tab.Shell(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tab.close)
		waitFor(t, func() bool {
			b, _ := os.ReadFile(log)
			return strings.Contains(string(b), kind+" env=")
		})
	}
	defaults := filepath.Join(reportDir("qwen"), "defaults.json")
	if got, _ := os.ReadFile(log); string(got) != "qwen env="+defaults+"\ncodex env=unset\n" {
		t.Fatalf("log:\n%s", got)
	}
	if _, err := os.Stat(defaults); err != nil {
		t.Fatalf("the qwen tab is pointed at %s, which punk0 did not write: %v", defaults, err)
	}
}
